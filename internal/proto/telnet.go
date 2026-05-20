package proto

// Telnet protocol constants (RFC 854 + option RFCs).

// Commands
const (
	telnetSE   byte = 240 // End of subnegotiation
	telnetNOP  byte = 241 // No operation
	telnetDM   byte = 242 // Data mark
	telnetBRK  byte = 243 // Break
	telnetGA   byte = 249 // Go ahead
	telnetSB   byte = 250 // Subnegotiation begin
	telnetWILL byte = 251 // Will (offer)
	telnetWONT byte = 252 // Won't
	telnetDO   byte = 253 // Do (request)
	telnetDONT byte = 254 // Don't
	telnetIAC  byte = 255 // Interpret as command
)

// Options
const (
	optEcho      byte = 1   // RFC 857
	optSGA       byte = 3   // Suppress go-ahead, RFC 858
	optStatus    byte = 5   // RFC 859
	optTermType  byte = 24  // RFC 1091
	optNAWS      byte = 31  // Negotiate about window size, RFC 1073
	optTermSpeed byte = 32  // RFC 1079
	optCompress2 byte = 86  // MCCP2 – MUD Client Compression Protocol v2
	optMXP       byte = 91  // MUD eXtension Protocol
	optGMCP      byte = 201 // Generic MUD Communication Protocol
)

// FSM states for the Telnet parser.
type fsmState uint8

const (
	stateData    fsmState = iota // Normal data
	stateIAC                     // Saw IAC (0xFF)
	stateWill                    // Saw IAC WILL — next byte is option
	stateWont                    // Saw IAC WONT
	stateDo                      // Saw IAC DO
	stateDont                    // Saw IAC DONT
	stateSB                      // Saw IAC SB — next byte is option
	stateSBData                  // Collecting subneg data
	stateSBIAC                   // Saw IAC inside subneg (maybe SE)
)
