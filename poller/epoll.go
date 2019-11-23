package poller

import (
	"golang.org/x/sys/unix"
)

const (
	EventRead  Event = 0x1
	EventWrite Event = 0x2
	EventErr   Event = 0x80
)

type (
	Event uint32

	Poll struct {
		fd            int
		eventFd       int
		wakeBytes     []byte
		wakeReadBytes []byte
		closeChan     chan struct{}
		close         bool
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
		fd:            fd,
		eventFd:       eventFd,
		wakeBytes:     []byte{1, 0, 0, 0, 0, 0, 0, 0},
		wakeReadBytes: make([]byte, 8),
		closeChan:     make(chan struct{}, 0),
		close:         false,
	}, nil
}

func (p *Poll) Wake() error {
	_, err := unix.Write(p.eventFd, p.wakeBytes)
	return err
}

func (p *Poll) wakeHandlerRead() {
	_, _ = unix.Read(p.eventFd, p.wakeReadBytes)
}

func (p *Poll) Close() error {
	if err := p.Wake(); err != nil {
		p.close = true
		return err
	}

	<-p.closeChan

	_ = unix.Close(p.fd)
	_ = unix.Close(p.eventFd)
	return nil
}

func (p *Poll) add(fd int, events uint32) error {
	return unix.EpollCtl(p.fd, unix.EPOLL_CTL_ADD, fd, &unix.EpollEvent{
		Events: events,
		Fd:     int32(fd),
	})
}

func (p *Poll) AddRead(fd int) error {
	return p.add(fd, unix.EPOLLIN|unix.EPOLLPRI)
}

func (p *Poll) AddWrite(fd int) error {
	return p.add(fd, unix.EPOLLOUT)
}

func (p *Poll) Del(fd int) error {
	return unix.EpollCtl(p.fd, unix.EPOLL_CTL_DEL, fd, nil)
}

func (p *Poll) mod(fd int, events uint32) error {
	return unix.EpollCtl(p.fd, unix.EPOLL_CTL_MOD, fd, &unix.EpollEvent{
		Events: events,
		Fd:     int32(fd),
	})
}

func (p *Poll) EnableReadWrite(fd int) error {
	return p.mod(fd, unix.EPOLLOUT|unix.EPOLLIN|unix.EPOLLPRI)
}

func (p *Poll) EnableRead(fd int) error {
	return p.mod(fd, unix.EPOLLIN|unix.EPOLLPRI)
}

func (p *Poll) EnableWrite(fd int) error {
	return p.mod(fd, unix.EPOLLOUT)
}

func (p *Poll) Run(handler func(fd int, event Event)) {
	defer func() {
		close(p.closeChan)
	}()

	var (
		events = make([]unix.EpollEvent, 1000)
		wake   = false
	)

	for {
		n, err := unix.EpollWait(p.fd, events, -1)
		if err != nil && err != unix.EINTR {
			continue
		}

		for i := 0; i < n; i++ {
			fd := int(events[i].Fd)
			if fd == p.eventFd {
				p.wakeHandlerRead()
				if p.close {
					return
				}
				wake = true
				continue
			}

			var cEvents Event
			if ((events[i].Events & unix.POLLHUP) != 0) && ((events[i].Events & unix.POLLIN) == 0) {
				cEvents |= EventErr
			}
			if (events[i].Events&unix.EPOLLERR != 0) || (events[i].Events&unix.EPOLLOUT != 0) {
				cEvents |= EventWrite
			}
			if events[i].Events&(unix.EPOLLIN|unix.EPOLLPRI|unix.EPOLLRDHUP) != 0 {
				cEvents |= EventRead
			}

			handler(fd, cEvents)
		}

		if wake {
			handler(-1, 0)
			wake = false
		}
	}
}
