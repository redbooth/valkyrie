// Copyright (c) 2015, Air Computing Inc. <oss@aerofs.com>
// All rights reserved.

package main

import (
	"bytes"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net"
	"os"
	"strconv"
	"sync"
	"time"
)

type server struct {
	UseSplice bool

	l net.Listener

	c map[uint32]*conn

	connections sync.RWMutex
	bufferPool  sync.Pool
}

type conn struct {
	s  *server
	id uint32
	c  net.Conn

	binding *conn
	barrier chan bool
}

func main() {
	var port int
	var useSplice bool

	flag.IntVar(&port, "port", 8888, "listening port")
	flag.BoolVar(&useSplice, "splice", true, "zero-copy proxy w/ splice syscall")
	flag.Parse()

	rand.Seed(time.Now().Unix())

	fmt.Println("Valkyrie serving at", port)
	l, err := net.Listen("tcp", "0.0.0.0:"+strconv.Itoa(port))
	if err != nil {
		panic("failed: " + err.Error())
	}
	s := NewServer(l)
	s.UseSplice = useSplice
	err = s.serve()
	if err != nil {
		panic(err)
	}
}

func NewServer(l net.Listener) *server {
	return &server{
		l: l,
		c: make(map[uint32]*conn),
		bufferPool: sync.Pool{
			New: func() interface{} {
				return make([]byte, 1024)
			},
		},
	}
}

func (s *server) serve() error {
	for {
		c, err := s.l.Accept()
		if err != nil {
			return err
		}
		cc := s.newConnection(c)
		if cc != nil {
			go cc.handle()
		}
	}
}

func (s *server) assignId(c *conn) error {
	s.connections.Lock()
	for i := 0; i < 10; i++ {
		id := rand.Uint32()
		if s.c[id] == nil {
			s.c[id] = c
			s.connections.Unlock()
			c.id = id
			return nil
		}
	}
	s.connections.Unlock()
	return fmt.Errorf("unable to assign id")
}

func (s *server) newConnection(c net.Conn) *conn {
	cc := &conn{
		s: s,
		c: c,

		barrier: make(chan bool),
	}
	if err := s.assignId(cc); err != nil {
		c.Close()
		return nil
	}
	if tcp, ok := c.(*net.TCPConn); ok {
		tcp.SetNoDelay(true)
		tcp.SetLinger(0)
	}
	return cc
}

var header []byte = []byte{
	// magic
	0x82, 0x96, 0x44, 0xa1,
	// payload length ( big endian)
	0, 0, 0, 4,
}

func (c *conn) handle() {
	d := []byte{
		// magic
		0x82, 0x96, 0x44, 0xa1,
		// payload length ( big endian)
		0, 0, 0, 4,
		// zid (big endian)
		byte((c.id >> 24) & 0xff),
		byte((c.id >> 16) & 0xff),
		byte((c.id >> 8) & 0xff),
		byte(c.id & 0xff),
	}
	n, err := c.c.Write(d)
	if err != nil || n != 12 {
		fmt.Println("failed to write zid:", err)
		c.Close()
		return
	}

	c.c.SetReadDeadline(time.Now().Add(15 * time.Second))
	r := 0
	for err == nil && r < 12 {
		n, err = c.c.Read(d[r:])
		r += n
	}
	if err != nil || r != 12 {
		if err != io.EOF {
			fmt.Println("failed to read bind req:", err, r, hex.EncodeToString(d))
		}
		c.Close()
		return
	}
	if !bytes.Equal(d[0:8], header) {
		fmt.Println("invalid bind req")
		c.Close()
		return
	}

	zid := uint32(d[8])<<24 | uint32(d[9])<<16 | uint32(d[10])<<8 | uint32(d[11])

	if zid == c.id {
		fmt.Println("cannot self-bind")
		c.Close()
		return
	}

	c.s.connections.RLock()
	peer := c.s.c[zid]
	c.s.connections.RUnlock()

	if peer == nil {
		fmt.Println("unknown zid in bind req", zid)
		c.Close()
		return
	}
	c.binding = peer

	// sync on other connection receiving matching bind req
	close(c.barrier)
	select {
	case _ = <-peer.barrier:
		if peer.binding != c {
			fmt.Println("mismatched binding", c.id, "->", zid, "->", peer.binding.id)
			c.binding = nil
			c.Close()
			return
		}
	case _ = <-time.After(30 * time.Second):
		fmt.Println("bind timed out", c.id, "->", zid)
		c.binding = nil
		c.Close()
		return
	}
	if c.id < zid {
		fmt.Println("bound", c.id, "<->", zid)
	}

	// proxy loop
	// reset read deadline
	c.c.SetReadDeadline(time.Time{})

	if tcp, ok := peer.c.(*net.TCPConn); ok && c.s.UseSplice {
		// NB: this is only worth doing if the runtime uses splice() under the hood
		// which currently requires a patch to the standard library...
		_, err = tcp.ReadFrom(c.c)
	} else {
		buf := c.s.bufferPool.Get().([]byte)
		for {
			n, err = c.c.Read(buf)
			if err != nil {
				break
			}

			n, err = peer.c.Write(buf[0:n])
			if err != nil {
				break
			}
		}
		c.s.bufferPool.Put(buf)
	}
	if err != nil {
		// filter out expected errors
		if nerr, ok := err.(*net.OpError); ok && isErrClosing(nerr.Err) {
		} else if serr, ok := err.(*os.SyscallError); ok && isErrClosing(serr.Err) {
		} else if err != io.EOF && !isErrClosing(err) {
			fmt.Println("copy failed:", c.id, "->", err)
		}
	}
	c.Close()
}

// sigh, why wouldn't they export errClosing...
func isErrClosing(err error) bool {
	return err.Error() == "use of closed network connection"
}

func (c *conn) Close() {
	c.c.Close()
	if c.binding != nil {
		c.binding.c.Close()
	}
	c.s.connections.Lock()
	delete(c.s.c, c.id)
	c.s.connections.Unlock()
}
