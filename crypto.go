package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"fmt"
	"math/big"
	"time"
)

type TLSManager struct {
	Certificate tls.Certificate
	Fingerprint string
}

// 在内存中动态生成临时自签名 TLS 1.3 证书 (零临时文件落盘，纯内存保障)
func NewTLSManager() (*TLSManager, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("生成 ECDSA 私钥失败: %w", err)
	}

	notBefore := time.Now().Add(-10 * time.Minute)
	notAfter := notBefore.Add(365 * 24 * time.Hour)

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return nil, fmt.Errorf("生成证书序列号失败: %w", err)
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"GhostTunnel P2P Network"},
			CommonName:   "ghosttunnel.local",
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return nil, fmt.Errorf("自签名证书创建失败: %w", err)
	}

	cert := tls.Certificate{
		Certificate: [][]byte{derBytes},
		PrivateKey:  priv,
	}

	// 计算 SHA-256 指纹
	hash := sha256.Sum256(derBytes)
	fingerprint := hex.EncodeToString(hash[:])

	return &TLSManager{
		Certificate: cert,
		Fingerprint: fingerprint,
	}, nil
}

// 获取服务端 TLS 配置 (强制 TLS 1.3)
func (tm *TLSManager) GetServerTLSConfig() *tls.Config {
	return &tls.Config{
		Certificates: []tls.Certificate{tm.Certificate},
		MinVersion:   tls.VersionTLS13,
	}
}

// 获取客户端 TLS 配置 (强制 TLS 1.3，跳过 CA 验证以支持自签名对等直连)
func (tm *TLSManager) GetClientTLSConfig() *tls.Config {
	return &tls.Config{
		MinVersion:         tls.VersionTLS13,
		InsecureSkipVerify: true,
	}
}
