package script

// Callbacks supplied by the caller to give the bridge access to GoFugue state.
type Callbacks struct {
	Send            func(world, text string) error
	Echo            func(text string)
	ForegroundWorld func() string
	Getvar          func(name string) (string, bool)
	Setvar          func(name, value string)
}
