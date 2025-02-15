package derr

type Error struct {
	Out string
	In  string
}

func (e Error) Error() string {
	return e.In
}
