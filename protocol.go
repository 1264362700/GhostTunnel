package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// 二进制帧传输协议 (Framing Protocol)
// 0~3   (4B): MagicHeader 0x47485354 ("GHST")
// 4     (1B): MsgType     (0x01: 握手, 0x02: 文本/剪贴板, 0x09: 心跳)
// 5~12  (8B): StreamID    会话流唯一标识
// 13~20 (8B): PayloadLen  数据载荷长度 N
// 21~21+N:   Payload     载荷数据

const (
	MagicHeader uint32 = 0x47485354 // "GHST"

	MsgTypeHandshake uint8 = 0x01 // 握手帧
	MsgTypeText      uint8 = 0x02 // 即焚剪贴流/文本
	MsgTypeImage     uint8 = 0x03 // 即焚剪贴流/图片 (PNG/JPEG)
	MsgTypeFileMeta  uint8 = 0x04 // 大文件/文件夹元数据帧
	MsgTypeFileChunk uint8 = 0x05 // 大文件/文件夹二进制分块数据帧
	MsgTypePing      uint8 = 0x09 // 心跳探活
)

var (
	ErrInvalidMagic   = errors.New("无效的协议魔数")
	ErrPayloadTooLong = errors.New("载荷长度超出单帧上限")
)

const MaxPayloadSize = 64 * 1024 * 1024 // 单帧最大 64MB

type Frame struct {
	MsgType  uint8
	StreamID uint64
	Payload  []byte
}

type FileMeta struct {
	StreamID uint64 `json:"stream_id"`
	Filename string `json:"filename"`
	FileSize int64  `json:"file_size"`
	IsFolder bool   `json:"is_folder"`
}

type TransferProgress struct {
	StreamID   uint64  `json:"stream_id"`
	Filename   string  `json:"filename"`
	TotalBytes int64   `json:"total_bytes"`
	SentBytes  int64   `json:"sent_bytes"`
	SpeedBps   float64 `json:"speed_bps"`
	Percent    float64 `json:"percent"`
	IsComplete bool    `json:"is_complete"`
}

// 封包编码为二进制字节流并写入对端连接
func EncodeFrame(w io.Writer, f Frame) error {
	payloadLen := uint64(len(f.Payload))
	if payloadLen > MaxPayloadSize {
		return ErrPayloadTooLong
	}

	header := make([]byte, 21)
	binary.BigEndian.PutUint32(header[0:4], MagicHeader)
	header[4] = f.MsgType
	binary.BigEndian.PutUint64(header[5:13], f.StreamID)
	binary.BigEndian.PutUint64(header[13:21], payloadLen)

	if _, err := w.Write(header); err != nil {
		return fmt.Errorf("写入帧头失败: %w", err)
	}

	if payloadLen > 0 {
		if _, err := w.Write(f.Payload); err != nil {
			return fmt.Errorf("写入数据载荷失败: %w", err)
		}
	}

	return nil
}

// 从连接字节流解码还原数据帧
func DecodeFrame(r io.Reader) (Frame, error) {
	header := make([]byte, 21)
	if _, err := io.ReadFull(r, header); err != nil {
		return Frame{}, err
	}

	magic := binary.BigEndian.Uint32(header[0:4])
	if magic != MagicHeader {
		return Frame{}, ErrInvalidMagic
	}

	msgType := header[4]
	streamID := binary.BigEndian.Uint64(header[5:13])
	payloadLen := binary.BigEndian.Uint64(header[13:21])

	if payloadLen > MaxPayloadSize {
		return Frame{}, ErrPayloadTooLong
	}

	payload := make([]byte, payloadLen)
	if payloadLen > 0 {
		if _, err := io.ReadFull(r, payload); err != nil {
			return Frame{}, fmt.Errorf("读取载荷数据失败: %w", err)
		}
	}

	return Frame{
		MsgType:  msgType,
		StreamID: streamID,
		Payload:  payload,
	}, nil
}
