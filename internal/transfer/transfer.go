package transfer

import (
	"bufio"
	"context"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"runtime"
	"sync"

	"hayate/internal/crypto"

	"github.com/klauspost/compress/zstd"
	"github.com/quic-go/quic-go"
)

const Version = "2.0.0"

const (
	frameFlagRaw  byte = 0x00
	frameFlagZstd byte = 0x01

	maxFilenameBytes          = 4096
	maxCompressedOverhead     = 4*1024*1024/8 + 64*1024
	maxMetadataCiphertextSize = 2 + maxFilenameBytes + 8 + 12 + 16
	maxInt64                  = int64(^uint64(0) >> 1)
)

var (
	// 4MB chunks reduce QUIC framing overhead vs 1MB and better saturate fast links.
	ChunkSize = 4 * 1024 * 1024

	// Scale workers to actual hardware; capped to prevent goroutine thrashing.
	NumWorkers = clampWorkers(runtime.NumCPU())

	// Pipeline queue depth controls backpressure between stages.
	MaxQueue = 16
)

func clampWorkers(n int) int {
	if n < 2 {
		return 2
	}
	if n > 16 {
		return 16
	}
	return n
}

var (
	chunkPool = sync.Pool{
		New: func() any {
			b := make([]byte, ChunkSize)
			return &b
		},
	}
	// 4-byte frame header prefix + encrypted 1-byte frame flag + chunk + zstd overhead + AEAD nonce (12) + tag (16).
	cipherPool = sync.Pool{
		New: func() any {
			b := make([]byte, 4+12+1+ChunkSize+maxCompressedOverhead+16)
			return &b
		},
	}
)

type chunkJob struct {
	index int64
	data  []byte
	size  int
}

type chunkResult struct {
	index      int64
	frame      []byte // [4-byte len header][encrypted payload] — single write
	frameOwner *[]byte
	rawN       int // original plaintext bytes for accurate progress
	err        error
}

// recvJob carries a raw encrypted frame read from the network.
type recvJob struct {
	index     int64
	data      []byte
	dataOwner *[]byte
}

// recvResult carries decrypted+decompressed plaintext ready for sequential disk write.
type recvResult struct {
	index      int64
	plaintext  []byte
	plainOwner *[]byte
	err        error
}

// SendFile reads the source file, compresses and encrypts chunks in parallel,
// and streams them over QUIC using coalesced frame writes.
// Progress callback receives plaintext bytes consumed.
func SendFile(ctx context.Context, path string, stream *quic.Stream, key []byte, compressMode string, progressCb func(int64)) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("opening file: %w", err)
	}
	defer file.Close()
	reader := bufio.NewReaderSize(file, ChunkSize)

	aead, err := crypto.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("creating cipher: %w", err)
	}
	ng, err := crypto.NewNonceGen()
	if err != nil {
		return "", fmt.Errorf("creating nonce gen: %w", err)
	}

	compressChunks := ShouldCompress(path, compressMode)
	var zstdEncoder *zstd.Encoder
	if compressChunks {
		zstdEncoder, err = zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedFastest), zstd.WithEncoderConcurrency(1))
		if err != nil {
			return "", fmt.Errorf("initializing zstd encoder: %w", err)
		}
		defer zstdEncoder.Close()
	}

	shaHasher := sha256.New()
	jobs := make(chan chunkJob, MaxQueue)
	results := make(chan chunkResult, MaxQueue)
	var wg sync.WaitGroup

	for i := 0; i < NumWorkers; i++ {
		wg.Add(1)
		go sendWorker(ctx, &wg, aead, ng, zstdEncoder, compressMode, compressChunks, jobs, results)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	readErrChan := make(chan error, 1)
	go func() {
		defer close(jobs)
		var index int64
		for {
			bufPtr := chunkPool.Get().(*[]byte)
			if cap(*bufPtr) < ChunkSize {
				b := make([]byte, ChunkSize)
				bufPtr = &b
			}
			n, err := reader.Read((*bufPtr)[:ChunkSize])
			if n > 0 {
				shaHasher.Write((*bufPtr)[:n])
				select {
				case jobs <- chunkJob{index: index, data: *bufPtr, size: n}:
					index++
				case <-ctx.Done():
					return
				}
			} else {
				chunkPool.Put(bufPtr)
			}
			if err != nil {
				if err == io.EOF {
					break
				}
				readErrChan <- fmt.Errorf("reading file: %w", err)
				return
			}
		}
		readErrChan <- nil
	}()

	var nextIndex int64
	pending := make(map[int64]chunkResult)
	var totalPlaintext int64

	for {
		select {
		case res, ok := <-results:
			if !ok {
				goto checkReader
			}
			if res.err != nil {
				return "", fmt.Errorf("worker chunk %d: %w", res.index, res.err)
			}
			pending[res.index] = res

			for {
				r, exists := pending[nextIndex]
				if !exists {
					break
				}

				if _, err := stream.Write(r.frame); err != nil {
					return "", fmt.Errorf("writing frame: %w", err)
				}

				cipherPool.Put(r.frameOwner)
				delete(pending, nextIndex)
				nextIndex++

				totalPlaintext += int64(r.rawN)
				progressCb(totalPlaintext)
			}

		case <-ctx.Done():
			return "", ctx.Err()
		}
	}

checkReader:
	if err := <-readErrChan; err != nil {
		return "", err
	}

	return hex.EncodeToString(shaHasher.Sum(nil)), nil
}

func sendWorker(ctx context.Context, wg *sync.WaitGroup, aead cipher.AEAD, ng *crypto.NonceGen, enc *zstd.Encoder, compressMode string, compressChunks bool, jobs <-chan chunkJob, results chan<- chunkResult) {
	defer wg.Done()
	var compBuf []byte
	var plainFrame []byte

	for job := range jobs {
		payload := job.data[:job.size]
		flag := frameFlagRaw
		if compressChunks {
			compBuf = enc.EncodeAll(payload, compBuf[:0])
			if compressMode == CompressAlways || len(compBuf) < len(payload) {
				payload = compBuf
				flag = frameFlagZstd
			}
		}
		rawN := job.size

		outPtr := cipherPool.Get().(*[]byte)
		outBuf := *outPtr

		if cap(plainFrame) < 1+len(payload) {
			plainFrame = make([]byte, 1+len(payload))
		}
		plainFrame = plainFrame[:1+len(payload)]
		plainFrame[0] = flag
		copy(plainFrame[1:], payload)
		chunkPool.Put(&job.data)

		// Reserve 4 bytes for frame header at the front; encrypt into outBuf[4:].
		required := 4 + aead.NonceSize() + len(plainFrame) + aead.Overhead()
		if cap(outBuf) < required {
			b := make([]byte, required)
			outPtr = &b
			outBuf = *outPtr
		}
		encSlice := outBuf[4:]
		sealed, err := crypto.EncryptInPlace(aead, ng, plainFrame, encSlice)
		if err != nil {
			cipherPool.Put(outPtr)
			select {
			case results <- chunkResult{index: job.index, err: err}:
			case <-ctx.Done():
			}
			return
		}

		// Write frame header [4-byte payload length] directly before the sealed data
		binary.BigEndian.PutUint32(outBuf[:4], uint32(len(sealed)))
		frame := outBuf[:4+len(sealed)]

		select {
		case results <- chunkResult{index: job.index, frame: frame, frameOwner: outPtr, rawN: rawN}:
		case <-ctx.Done():
			cipherPool.Put(outPtr)
			return
		}
	}
}

// ReceiveFile reads encrypted frames from QUIC, decrypts and decompresses
// in parallel across NumWorkers goroutines, then writes plaintext to disk
// in sequential order. Progress callback receives plaintext bytes written.
func ReceiveFile(ctx context.Context, path string, stream *quic.Stream, key []byte, expectedSize int64, progressCb func(int64)) (string, error) {
	file, err := os.Create(path)
	if err != nil {
		return "", fmt.Errorf("creating file: %w", err)
	}
	defer file.Close()

	aead, err := crypto.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("creating cipher: %w", err)
	}

	zstdDecoder, err := zstd.NewReader(nil)
	if err != nil {
		return "", fmt.Errorf("initializing zstd decoder: %w", err)
	}
	defer zstdDecoder.Close()

	recvJobs := make(chan recvJob, MaxQueue)
	recvResults := make(chan recvResult, MaxQueue)
	var wg sync.WaitGroup

	for i := 0; i < NumWorkers; i++ {
		wg.Add(1)
		go recvWorker(ctx, &wg, aead, zstdDecoder, recvJobs, recvResults)
	}

	go func() {
		wg.Wait()
		close(recvResults)
	}()

	// Network reader goroutine: reads frames and dispatches to workers
	readErrChan := make(chan error, 1)
	go func() {
		defer close(recvJobs)
		headerBuf := make([]byte, 4)
		var index int64
		for {
			_, err := io.ReadFull(stream, headerBuf)
			if err != nil {
				if err == io.EOF || err == io.ErrUnexpectedEOF {
					break
				}
				readErrChan <- fmt.Errorf("reading frame header: %w", err)
				return
			}
			chunkLen := binary.BigEndian.Uint32(headerBuf)
			maxFrameLen := uint32(aead.NonceSize() + 1 + ChunkSize + maxCompressedOverhead + aead.Overhead())
			if chunkLen == 0 || chunkLen > maxFrameLen {
				readErrChan <- fmt.Errorf("invalid frame payload length: %d", chunkLen)
				return
			}

			bufPtr := cipherPool.Get().(*[]byte)
			if uint32(cap(*bufPtr)) < chunkLen {
				b := make([]byte, chunkLen)
				bufPtr = &b
			}
			buf := (*bufPtr)[:chunkLen]

			if _, err := io.ReadFull(stream, buf); err != nil {
				cipherPool.Put(bufPtr)
				readErrChan <- fmt.Errorf("reading frame payload: %w", err)
				return
			}

			select {
			case recvJobs <- recvJob{index: index, data: buf, dataOwner: bufPtr}:
				index++
			case <-ctx.Done():
				cipherPool.Put(bufPtr)
				return
			}
		}
		readErrChan <- nil
	}()

	// Sequencer: write decrypted chunks to disk in order
	shaHasher := sha256.New()
	var nextIndex int64
	pending := make(map[int64]recvResult)
	var totalReceived int64

	for {
		select {
		case res, ok := <-recvResults:
			if !ok {
				goto checkReader
			}
			if res.err != nil {
				return "", fmt.Errorf("worker chunk %d: %w", res.index, res.err)
			}
			pending[res.index] = res

			for {
				r, exists := pending[nextIndex]
				if !exists {
					break
				}

				if _, err := file.Write(r.plaintext); err != nil {
					return "", fmt.Errorf("writing to disk: %w", err)
				}
				shaHasher.Write(r.plaintext)

				totalReceived += int64(len(r.plaintext))
				progressCb(totalReceived)

				chunkPool.Put(r.plainOwner)
				delete(pending, nextIndex)
				nextIndex++

				if totalReceived >= expectedSize {
					goto flush
				}
			}

		case <-ctx.Done():
			return "", ctx.Err()
		}
	}

flush:
checkReader:
	select {
	case err := <-readErrChan:
		if err != nil {
			return "", err
		}
	default:
	}

	if err := file.Sync(); err != nil {
		return "", fmt.Errorf("syncing file: %w", err)
	}

	return hex.EncodeToString(shaHasher.Sum(nil)), nil
}

func recvWorker(ctx context.Context, wg *sync.WaitGroup, aead cipher.AEAD, dec *zstd.Decoder, jobs <-chan recvJob, results chan<- recvResult) {
	defer wg.Done()
	var compBuf []byte

	for job := range jobs {
		// Decrypt in-place into reusable compBuf
		needed := len(job.data) - aead.NonceSize() - aead.Overhead()
		if needed < 0 {
			needed = 0
		}
		if cap(compBuf) < needed {
			compBuf = make([]byte, needed)
		}

		decData, err := crypto.DecryptInPlace(aead, job.data, compBuf)
		cipherPool.Put(job.dataOwner)
		if err != nil {
			select {
			case results <- recvResult{index: job.index, err: err}:
			case <-ctx.Done():
			}
			return
		}

		if len(decData) < 1 {
			select {
			case results <- recvResult{index: job.index, err: fmt.Errorf("frame missing compression flag")}:
			case <-ctx.Done():
			}
			return
		}

		flag := decData[0]
		payload := decData[1:]

		decompPtr := chunkPool.Get().(*[]byte)
		var plaintext []byte
		switch flag {
		case frameFlagRaw:
			if cap(*decompPtr) < len(payload) {
				b := make([]byte, ChunkSize)
				if cap(b) < len(payload) {
					b = make([]byte, len(payload))
				}
				decompPtr = &b
			}
			plaintext = (*decompPtr)[:len(payload)]
			copy(plaintext, payload)
		case frameFlagZstd:
			var err error
			plaintext, err = dec.DecodeAll(payload, (*decompPtr)[:0])
			if err != nil {
				chunkPool.Put(decompPtr)
				select {
				case results <- recvResult{index: job.index, err: fmt.Errorf("decompressing: %w", err)}:
				case <-ctx.Done():
				}
				return
			}
		default:
			chunkPool.Put(decompPtr)
			select {
			case results <- recvResult{index: job.index, err: fmt.Errorf("invalid frame compression flag: 0x%02x", flag)}:
			case <-ctx.Done():
			}
			return
		}

		select {
		case results <- recvResult{index: job.index, plaintext: plaintext, plainOwner: decompPtr}:
		case <-ctx.Done():
			chunkPool.Put(decompPtr)
			return
		}
	}
}

// EstablishSecureStreamSender performs ephemeral key exchange and sends file metadata.
func EstablishSecureStreamSender(ctx context.Context, stream *quic.Stream, filename string, fileSize int64) ([]byte, error) {
	priv, pub, err := crypto.GenerateEphemeralKeyPair()
	if err != nil {
		return nil, fmt.Errorf("generating keypair: %w", err)
	}

	if _, err := stream.Write(pub); err != nil {
		return nil, fmt.Errorf("sending public key: %w", err)
	}

	peerPub := make([]byte, 32)
	if _, err := io.ReadFull(stream, peerPub); err != nil {
		return nil, fmt.Errorf("reading peer public key: %w", err)
	}

	key, err := crypto.DeriveSharedSecret(priv, peerPub)
	if err != nil {
		return nil, fmt.Errorf("deriving shared secret: %w", err)
	}

	filenameBytes := []byte(filename)
	if len(filenameBytes) == 0 {
		return nil, fmt.Errorf("filename is empty")
	}
	if len(filenameBytes) > maxFilenameBytes {
		return nil, fmt.Errorf("filename too long: %d bytes", len(filenameBytes))
	}
	metadataBytes := make([]byte, 2+len(filenameBytes)+8)
	binary.BigEndian.PutUint16(metadataBytes[0:2], uint16(len(filenameBytes)))
	copy(metadataBytes[2:2+len(filenameBytes)], filenameBytes)
	binary.BigEndian.PutUint64(metadataBytes[2+len(filenameBytes):], uint64(fileSize))

	encMetadata, err := crypto.Encrypt(key, metadataBytes)
	if err != nil {
		return nil, fmt.Errorf("encrypting metadata: %w", err)
	}

	lengthBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lengthBuf, uint32(len(encMetadata)))
	if _, err := stream.Write(lengthBuf); err != nil {
		return nil, fmt.Errorf("writing metadata length: %w", err)
	}
	if _, err := stream.Write(encMetadata); err != nil {
		return nil, fmt.Errorf("writing metadata payload: %w", err)
	}

	return key, nil
}

// EstablishSecureStreamReceiver performs ephemeral key exchange and receives file metadata.
func EstablishSecureStreamReceiver(ctx context.Context, stream *quic.Stream) ([]byte, string, int64, error) {
	priv, pub, err := crypto.GenerateEphemeralKeyPair()
	if err != nil {
		return nil, "", 0, fmt.Errorf("generating keypair: %w", err)
	}

	peerPub := make([]byte, 32)
	if _, err := io.ReadFull(stream, peerPub); err != nil {
		return nil, "", 0, fmt.Errorf("reading peer public key: %w", err)
	}

	if _, err := stream.Write(pub); err != nil {
		return nil, "", 0, fmt.Errorf("sending public key: %w", err)
	}

	key, err := crypto.DeriveSharedSecret(priv, peerPub)
	if err != nil {
		return nil, "", 0, fmt.Errorf("deriving shared secret: %w", err)
	}

	lengthBuf := make([]byte, 4)
	if _, err := io.ReadFull(stream, lengthBuf); err != nil {
		return nil, "", 0, fmt.Errorf("reading metadata length: %w", err)
	}
	encLen := binary.BigEndian.Uint32(lengthBuf)
	if encLen == 0 || encLen > maxMetadataCiphertextSize {
		return nil, "", 0, fmt.Errorf("invalid metadata length: %d", encLen)
	}

	encMetadata := make([]byte, encLen)
	if _, err := io.ReadFull(stream, encMetadata); err != nil {
		return nil, "", 0, fmt.Errorf("reading metadata payload: %w", err)
	}

	metadataBytes, err := crypto.Decrypt(key, encMetadata)
	if err != nil {
		return nil, "", 0, fmt.Errorf("decrypting metadata: %w", err)
	}

	if len(metadataBytes) < 10 {
		return nil, "", 0, fmt.Errorf("metadata too short: %d bytes", len(metadataBytes))
	}

	nameLen := binary.BigEndian.Uint16(metadataBytes[0:2])
	if len(metadataBytes) < int(2+nameLen+8) {
		return nil, "", 0, fmt.Errorf("metadata truncated: got %d, expected %d", len(metadataBytes), 2+nameLen+8)
	}

	if nameLen == 0 || nameLen > maxFilenameBytes {
		return nil, "", 0, fmt.Errorf("invalid filename length: %d", nameLen)
	}
	filename := string(metadataBytes[2 : 2+nameLen])
	rawSize := binary.BigEndian.Uint64(metadataBytes[2+nameLen : 2+nameLen+8])
	if rawSize > uint64(maxInt64) {
		return nil, "", 0, fmt.Errorf("file size too large: %d", rawSize)
	}
	fileSize := int64(rawSize)

	return key, filename, fileSize, nil
}
