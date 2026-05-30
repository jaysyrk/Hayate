package crypto

import (
	"crypto/rand"
	"encoding/binary"
	"strings"

	"golang.org/x/crypto/argon2"
)

var wordlist = []string{
	"apple", "bacon", "cabin", "dance", "eagle", "fable", "ghost", "habit",
	"ice", "juice", "kite", "lemon", "magic", "ninja", "ocean", "piano",
	"queen", "robot", "salad", "tiger", "uncle", "virus", "wagon", "zebra",
	"actor", "beach", "camel", "delta", "earth", "flame", "giant", "honey",
	"image", "jelly", "koala", "laser", "mango", "novel", "onion", "panda",
	"quiet", "radar", "snake", "train", "union", "vocal", "water", "yield",
	"alarm", "bread", "candy", "dream", "early", "field", "glass", "happy",
	"index", "jump", "king", "light", "mouse", "night", "orbit", "plant",
	"quick", "river", "smile", "table", "urban", "voice", "wheat", "young",
	"alien", "brick", "cause", "drill", "eager", "flash", "globe", "heart",
	"iron", "judge", "knife", "limit", "music", "noise", "order", "paper",
	"quote", "rock", "smoke", "taste", "usage", "volts", "wheel", "youth",
	"anger", "brush", "chain", "drink", "empty", "flock", "glory", "heavy",
	"ivory", "juice", "knock", "local", "naked", "north", "organ", "party",
	"radio", "rough", "solid", "theme", "usher", "vowel", "white", "zebra",
	"angle", "build", "chair", "drive", "enemy", "floor", "glove", "hedge",
	"jeans", "joint", "known", "logic", "nerve", "nurse", "other", "peace",
	"reach", "round", "sound", "thick", "valid", "watch", "whole", "zonal",
	"animal", "bunch", "chalk", "drone", "entry", "fluid", "grace", "hello",
	"jewel", "joker", "label", "loose", "never", "nylon", "outer", "pearl",
	"react", "route", "space", "thing", "value", "water", "width", "zones",
	"ankle", "buyer", "charm", "drown", "equal", "focus", "grain", "hinge",
	"jumbo", "jolly", "labor", "lower", "newly", "oasis", "owner", "phase",
	"ready", "royal", "speed", "think", "valve", "wealth", "woman", "zoom",
	"apple", "cable", "chart", "drum", "error", "force", "grand", "hobby",
	"jump", "joyful", "laced", "lucky", "night", "ocean", "paint", "phone",
	"rebel", "rural", "spend", "third", "vault", "weapon", "world", "zone",
	"armor", "cacao", "chase", "drunk", "event", "forge", "grant", "honor",
	"jury", "judge", "lance", "lunar", "noble", "offer", "panel", "photo",
	"recap", "safer", "spice", "thorn", "vegan", "weary", "worry", "zinc",
	"arrow", "cache", "cheap", "dryer", "exact", "forth", "grasp", "horse",
	"just", "juice", "large", "lunch", "noise", "often", "panic", "piece",
	"relax", "saint", "spike", "those", "venom", "weave", "worse", "zero",
	"asset", "caddy", "check", "duck", "exist", "forty", "grass", "hotel",
	"keen", "jelly", "laser", "lurch", "north", "older", "paper", "pilot",
	"relay", "salad", "spill", "three", "venue", "wedge", "worth", "zeal",
	"audio", "cadet", "cheek", "dummy", "extra", "forum", "grave", "hound",
	"keep", "joker", "latch", "lured", "notch", "olive", "parka", "pitch",
	"relic", "salon", "spine", "throw", "verge", "weigh", "would", "zest",
	"audit", "cagey", "cheer", "dump", "fable", "found", "great", "house",
	"kept", "jumbo", "later", "lurid", "novel", "omega", "party", "pivot",
	"remit", "salsa", "spite", "thumb", "verse", "weird", "wound", "zone",
}

func GeneratePassphrase(numWords int) (string, error) {
	if numWords <= 0 {
		numWords = 4
	}
	words := make([]string, numWords)
	for i := 0; i < numWords; i++ {
		var b [2]byte
		if _, err := rand.Read(b[:]); err != nil {
			return "", err
		}
		idx := int(binary.BigEndian.Uint16(b[:])) % len(wordlist)
		words[i] = wordlist[idx]
	}
	return strings.Join(words, "-"), nil
}

func DeriveKEK(passphrase string, salt []byte) []byte {
	time := uint32(2)
	memory := uint32(64 * 1024)
	threads := uint8(2)
	keyLen := uint32(32)

	return argon2.IDKey([]byte(passphrase), salt, time, memory, threads, keyLen)
}
