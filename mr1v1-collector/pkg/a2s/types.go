package a2s

// ServerType indicates the kind of game server.
type ServerType int

const (
	ServerTypeUnknown      ServerType = iota
	ServerTypeDedicated
	ServerTypeNonDedicated
	ServerTypeSourceTV
)

func ParseServerType(b uint8) ServerType {
	switch b {
	case 'd':
		return ServerTypeDedicated
	case 'l':
		return ServerTypeNonDedicated
	case 'p':
		return ServerTypeSourceTV
	}
	return ServerTypeUnknown
}

func (t ServerType) String() string {
	switch t {
	case ServerTypeDedicated:
		return "Dedicated"
	case ServerTypeNonDedicated:
		return "Non-Dedicated"
	case ServerTypeSourceTV:
		return "SourceTV"
	default:
		return "Unknown"
	}
}

func (t ServerType) MarshalJSON() ([]byte, error) {
	return []byte(`"` + t.String() + `"`), nil
}

// ServerOS indicates the operating system of the game server.
type ServerOS int

const (
	ServerOSUnknown ServerOS = iota
	ServerOSLinux
	ServerOSWindows
	ServerOSMac
)

func ParseServerOS(b uint8) ServerOS {
	switch b {
	case 'l':
		return ServerOSLinux
	case 'w':
		return ServerOSWindows
	case 'm', 'o':
		return ServerOSMac
	}
	return ServerOSUnknown
}

func (os ServerOS) String() string {
	switch os {
	case ServerOSLinux:
		return "Linux"
	case ServerOSWindows:
		return "Windows"
	case ServerOSMac:
		return "Mac"
	default:
		return "Unknown"
	}
}

func (os ServerOS) MarshalJSON() ([]byte, error) {
	return []byte(`"` + os.String() + `"`), nil
}
