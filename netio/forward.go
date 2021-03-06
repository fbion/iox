package netio

import (
	"io"
	"iox/logger"
	"iox/option"
)

// Memory optimized
func CipherCopy(dst Ctx, src Ctx) (int64, error) {
	buffer := make([]byte, option.TCP_BUFFER_SIZE)
	var written int64
	var err error

	for {
		var nr int
		var er error

		nr, er = src.DecryptRead(buffer)

		if nr > 0 {
			logger.Info(" <== [%d bytes] ==", nr)

			var nw int
			var ew error

			nw, ew = dst.EncryptWrite(buffer[:nr])

			if nw > 0 {
				logger.Info(" == [%d bytes] ==> ", nw)
				written += int64(nw)
			}
			if ew != nil {
				err = ew
				break
			}
			if nr != nw {
				err = io.ErrShortWrite
				break
			}
		}
		if er != nil {
			if er != io.EOF {
				err = er
			}
			break
		}
	}

	return written, err
}

func PipeForward(ctxA Ctx, ctxB Ctx) {
	signal := make(chan struct{}, 1)

	go func() {
		CipherCopy(ctxA, ctxB)
		signal <- struct{}{}
	}()

	go func() {
		CipherCopy(ctxB, ctxA)
		signal <- struct{}{}
	}()

	<-signal
}

// This function will run forever
// If need to do performance optimization in future,
// I will consider a go-routine pool here, but
// this can lead to mutex-lock overhead
func ForwardUDP(ctxA Ctx, ctxB Ctx) {
	go func() {
		buffer := make([]byte, option.UDP_PACKET_MAX_SIZE)
		for {
			nr, _ := ctxA.DecryptRead(buffer)
			if nr > 0 {
				if nr == 4 &&
					buffer[0] == 0xCC && buffer[1] == 0xDD &&
					buffer[2] == 0xEE && buffer[3] == 0xFF {
					continue
				}
				logger.Info(" <== [%d bytes] ==", nr)
				nw, _ := ctxB.EncryptWrite(buffer[:nr])
				if nw > 0 {
					logger.Info(" == [%d bytes] ==>", nw)
				}
			}
		}
	}()

	go func() {
		buffer := make([]byte, option.UDP_PACKET_MAX_SIZE)
		for {
			nr, _ := ctxB.DecryptRead(buffer)
			if nr > 0 {
				if nr == 4 &&
					buffer[0] == 0xCC && buffer[1] == 0xDD &&
					buffer[2] == 0xEE && buffer[3] == 0xFF {
					continue
				}
				logger.Info(" <== [%d bytes] ==", nr)
				nw, _ := ctxA.EncryptWrite(buffer[:nr])
				if nw > 0 {
					logger.Info(" == [%d bytes] ==>", nw)
				}
			}
		}
	}()

	select {}
}

var UDP_INIT_PACKET = []byte{
	0xCC, 0xDD, 0xEE, 0xFF,
}

// Each socket only writes the packet to the address
// which last sent packet to it recently,
// instead of boardcasting to all the address.
// I think it is as expected
func ForwardUnconnectedUDP(ctxA Ctx, ctxB Ctx) {
	addrRegistedA := false
	addrRegistedB := false
	addrRegistedSignalA := make(chan struct{}, 1)
	addrRegistedSignalB := make(chan struct{}, 1)

	packetChannelA := make(chan []byte, option.UDP_PACKET_CHANNEL_SIZE)
	packetChannelB := make(chan []byte, option.UDP_PACKET_CHANNEL_SIZE)

	// A read
	go func() {
		for {
			buffer := make([]byte, option.UDP_PACKET_MAX_SIZE)
			nr, _ := ctxA.DecryptRead(buffer)
			if nr > 0 {
				if !addrRegistedA {
					addrRegistedA = true
					addrRegistedSignalA <- struct{}{}
				}

				if !(nr == 4 &&
					buffer[0] == 0xCC && buffer[1] == 0xDD &&
					buffer[2] == 0xEE && buffer[3] == 0xFF) {
					packetChannelB <- buffer[:nr]
				}
			}
		}
	}()

	// B read
	go func() {
		for {
			buffer := make([]byte, option.UDP_PACKET_MAX_SIZE)
			nr, _ := ctxB.DecryptRead(buffer)
			if nr > 0 {
				if !addrRegistedB {
					addrRegistedB = true
					addrRegistedSignalB <- struct{}{}
				}

				if !(nr == 4 &&
					buffer[0] == 0xCC && buffer[1] == 0xDD &&
					buffer[2] == 0xEE && buffer[3] == 0xFF) {
					packetChannelA <- buffer[:nr]
				}
			}
		}
	}()

	// A write
	go func() {
		<-addrRegistedSignalA
		for {
			packet := <-packetChannelA
			ctxA.EncryptWrite(packet)
		}
	}()

	// B write
	go func() {
		<-addrRegistedSignalB
		for {
			packet := <-packetChannelB
			ctxB.EncryptWrite(packet)
		}
	}()

	select {}
}
