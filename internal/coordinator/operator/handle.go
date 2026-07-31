package operator

import (
	"crypto/sha256"
	"encoding/binary"
)

// Host handles are the only host-facing identity the public API ever emits.
// They are derived from the node id with SHA-256 so the same machine always
// reads as the same name across refreshes and restarts, while the node id
// itself stays private: the hash is one-way and only 16 bits of it reach the
// output, so a handle cannot be walked back to an id.
//
// 64 adjectives x 64 animals is 4096 names. Collisions are possible and
// acceptable: the handle is a friendly label for a public ticker, never an
// identifier the platform resolves anything by.
var handleAdjectives = [64]string{
	"amber", "arctic", "ashen", "auburn", "azure", "basalt", "bronze", "cedar",
	"cobalt", "copper", "coral", "crimson", "dusk", "eastern", "ember", "fern",
	"flint", "frost", "garnet", "gilded", "glacier", "granite", "harbor", "hazel",
	"indigo", "ivory", "jade", "lunar", "maple", "marble", "midnight", "moss",
	"nimbus", "northern", "ochre", "onyx", "opal", "pewter", "quartz", "quiet",
	"rowan", "russet", "sable", "sandy", "scarlet", "sienna", "silent", "silver",
	"slate", "solar", "southern", "spruce", "steel", "stone", "teal", "thistle",
	"tidal", "topaz", "umber", "velvet", "verdant", "western", "willow", "winter",
}

var handleAnimals = [64]string{
	"albatross", "badger", "bison", "bittern", "buzzard", "caribou", "chamois", "cormorant",
	"crane", "curlew", "dipper", "dormouse", "eagle", "egret", "falcon", "finch",
	"fox", "gannet", "gecko", "godwit", "grebe", "harrier", "heron", "ibex",
	"ibis", "jackal", "jaguar", "kestrel", "kingfisher", "kite", "lapwing", "lark",
	"lynx", "marten", "merlin", "mink", "moorhen", "osprey", "otter", "owl",
	"panther", "petrel", "pika", "plover", "puffin", "quail", "raven", "redshank",
	"rook", "seal", "shrike", "siskin", "sparrow", "stoat", "swift", "tapir",
	"teal", "tern", "vireo", "viper", "vulture", "walrus", "weasel", "wren",
}

// HostHandle returns the stable anonymous display name for a node id.
func HostHandle(nodeID string) string {
	sum := sha256.Sum256([]byte(nodeID))
	adjective := handleAdjectives[binary.BigEndian.Uint16(sum[0:2])%uint16(len(handleAdjectives))]
	animal := handleAnimals[binary.BigEndian.Uint16(sum[2:4])%uint16(len(handleAnimals))]
	return adjective + "-" + animal
}
