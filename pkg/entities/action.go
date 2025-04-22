package entities

type Action struct {
	Kind ActionKind
	Note string
}

type ActionKind string

const (
	// ActionKindNoop is a noop action meaning nothing has to be done with a message
	ActionKindNoop = "noop"

	// ActionKindErase indicates that a message should be deleted
	ActionKindErase = "erase"

	// ActionKindBan indicates that a user should be banned
	ActionKindBan = "ban"
)
