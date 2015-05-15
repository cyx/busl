package broker

type channel string

func (c channel) id() string {
	return string(c) + ":id"
}

func (c channel) wildcardId() string {
	return string(c) + "*"
}

func (c channel) doneId() string {
	return string(c) + ":done"
}

func (c channel) killId() string {
	return string(c) + ":kill"
}
