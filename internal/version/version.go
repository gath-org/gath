package version

const (
	// Software is the milestone 1 release.
	Software = "0.1.0"
	// UserAgent identifies this implementation on the peer network.
	UserAgent = "gath/" + Software
	// Protocol is the P2P protocol version.
	Protocol uint32 = 1
)
