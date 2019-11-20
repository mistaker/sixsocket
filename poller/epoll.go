package main

import (
	"golang.org/x/sys/unix"
)

type (
	Poll struct {
		fd        int
		eventFd   int
		wakeBytes []byte
	}
)

func NewPoll() (*Poll, error) {
	fd, err := unix.EpollCreate1(0)
	if err != nil {
		return nil, err
	}

	r0, _, err := unix.Syscall(unix.SYS_EVENTFD2, 0, 0, 0)
	if err != nil {
		return nil, err
	}

	eventFd := int(r0)

	err = unix.EpollCtl(fd, unix.EPOLL_CTL_ADD, eventFd, &unix.EpollEvent{
		Events: unix.EPOLLIN,
		Fd:     int32(eventFd),
		Pad:    0,
	})

	if err != nil {
		_ = unix.Close(fd)
		_ = unix.Close(eventFd)
		return nil, err
	}

	return &Poll{
		fd:        fd,
		eventFd:   eventFd,
		wakeBytes: []byte{1, 0, 0, 0, 0, 0, 0, 0},
	}, nil
}

func (p *Poll) Wake() error {
	_, err := unix.Write(p.eventFd, p.wakeBytes)
	return err
}
