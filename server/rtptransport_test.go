package server

import (
	"net"
	"os"
	"testing"

	"github.com/peer-calls/peer-calls/server/logger"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v2/pkg/media"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newUDPServer() *net.UDPConn {
	laddr := &net.UDPAddr{
		Port: 1234,
		IP:   net.ParseIP("127.0.0.1"),
	}
	raddr := &net.UDPAddr{
		Port: 5678,
		IP:   net.ParseIP("127.0.0.1"),
	}

	conn, err := net.DialUDP("udp", laddr, raddr)
	if err != nil {
		panic(err)
	}
	return conn
}

func newUDPClient(addr net.Addr) *net.UDPConn {
	laddr := &net.UDPAddr{
		Port: 1234,
		IP:   net.ParseIP("127.0.0.1"),
	}
	raddr := &net.UDPAddr{
		Port: 5678,
		IP:   net.ParseIP("127.0.0.1"),
	}

	conn, err := net.DialUDP("udp", raddr, laddr)
	if err != nil {
		panic(err)
	}
	return conn
}

func TestUDP(t *testing.T) {
	conn1 := newUDPServer()
	conn2 := newUDPClient(conn1.LocalAddr())

	defer conn1.Close()
	defer conn2.Close()

	i, err := conn1.Write([]byte("ping"))
	assert.NoError(t, err)
	assert.Equal(t, 4, i)

	buf := make([]byte, 4)
	i, err = conn2.Read(buf)
	assert.NoError(t, err)
	assert.Equal(t, 4, i)
	assert.Equal(t, "ping", string(buf))

	i, err = conn2.Write([]byte("pong"))
	assert.NoError(t, err)
	assert.Equal(t, 4, i)

	i, err = conn1.Read(buf)
	assert.NoError(t, err)
	assert.Equal(t, 4, i)
	assert.Equal(t, "pong", string(buf))
}

func TestRTPTransport_RTP(t *testing.T) {
	conn1 := newUDPServer()
	conn2 := newUDPClient(conn1.LocalAddr())

	loggerFactory := logger.NewFactoryFromEnv("PEERCALLS_", os.Stderr)

	t1 := NewRTPTransport(loggerFactory, conn1)
	t2 := NewRTPTransport(loggerFactory, conn2)

	defer t1.Close()
	defer t2.Close()

	ssrc := uint32(123)

	packetizer := rtp.NewPacketizer(
		receiveMTU,
		96,
		ssrc,
		&codecs.VP8Payloader{},
		rtp.NewRandomSequencer(),
		96000,
	)

	writeSample := func(transport Transport, s media.Sample) []*rtp.Packet {
		pkts := packetizer.Packetize(s.Data, s.Samples)

		for _, pkt := range pkts {
			_, err := transport.WriteRTP(pkt)
			assert.NoError(t, err)
		}

		return pkts
	}

	sentPkts := writeSample(t1, media.Sample{Data: []byte{0x01}, Samples: 1})
	require.Equal(t, 1, len(sentPkts))

	expected := map[uint16][]byte{}
	for _, pkt := range sentPkts {
		b, err := pkt.Marshal()
		require.NoError(t, err)
		expected[pkt.SequenceNumber] = b
	}

	actual := map[uint16][]byte{}
	for i := 0; i < len(sentPkts); i++ {
		pkt := <-t2.RTPChannel()
		b, err := pkt.Marshal()
		require.NoError(t, err)
		actual[pkt.SequenceNumber] = b
	}

	assert.Equal(t, expected, actual)
}

func TestRTPTransport_RTCP(t *testing.T) {
	conn1 := newUDPServer()
	conn2 := newUDPClient(conn1.LocalAddr())

	loggerFactory := logger.NewFactoryFromEnv("PEERCALLS_", os.Stderr)

	t1 := NewRTPTransport(loggerFactory, conn1)
	t2 := NewRTPTransport(loggerFactory, conn2)

	defer t1.Close()
	defer t2.Close()

	senderReport := rtcp.SenderReport{
		SSRC: uint32(123),
	}

	writeRTCP := func(transport Transport, pkts []rtcp.Packet) {
		err := transport.WriteRTCP(pkts)
		require.NoError(t, err)
	}

	writeRTCP(t1, []rtcp.Packet{&senderReport})

	sentBytes, err := senderReport.Marshal()
	require.NoError(t, err)

	recvPkt := <-t2.RTCPChannel()

	recvBytes, err := recvPkt.Marshal()
	require.NoError(t, err)

	assert.Equal(t, sentBytes, recvBytes)
}
