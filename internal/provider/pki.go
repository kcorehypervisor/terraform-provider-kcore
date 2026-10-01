package provider

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

const (
	caValidity    = 3650 * 24 * time.Hour
	subCAValidity = 1825 * 24 * time.Hour
	leafValidity  = 365 * 24 * time.Hour
)

// clusterCertFiles matches the filenames `kctl create cluster` writes.
var clusterCertFiles = []string{
	"ca.crt", "ca.key",
	"sub-ca.crt", "sub-ca.key",
	"controller.crt", "controller.key",
	"kctl.crt", "kctl.key",
}

func hostFromAddress(addr string) string {
	if h, _, err := net.SplitHostPort(addr); err == nil {
		return h
	}
	return addr
}

func generateClusterPKI(dir, controllerAddr string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	host := hostFromAddress(controllerAddr)

	caKey, caCert, caCertPEM, caKeyPEM, err := newCA("kcore-cluster-ca", caValidity, -1)
	if err != nil {
		return fmt.Errorf("cluster CA: %w", err)
	}
	subKey, _, subCertPEM, subKeyPEM, err := newCA("kcore-cluster-sub-ca", subCAValidity, 0)
	if err != nil {
		return fmt.Errorf("sub-CA key: %w", err)
	}
	subCertPEM, err = signCert(subKey, caCert, caKey, "kcore-cluster-sub-ca", subCAValidity, true, 0, nil, nil)
	if err != nil {
		return fmt.Errorf("sub-CA: %w", err)
	}

	ctrlKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	ctrlCertPEM, ctrlKeyPEM, err := signLeaf(ctrlKey, caCert, caKey, "kcore-controller-"+host, host, true, true)
	if err != nil {
		return fmt.Errorf("controller cert: %w", err)
	}
	kctlKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	kctlCertPEM, kctlKeyPEM, err := signLeaf(kctlKey, caCert, caKey, "kctl", "", false, true)
	if err != nil {
		return fmt.Errorf("kctl cert: %w", err)
	}

	files := map[string]struct {
		pem  string
		mode os.FileMode
	}{
		"ca.crt":         {caCertPEM, 0o644},
		"ca.key":         {caKeyPEM, 0o600},
		"sub-ca.crt":     {subCertPEM, 0o644},
		"sub-ca.key":     {subKeyPEM, 0o600},
		"controller.crt": {ctrlCertPEM, 0o644},
		"controller.key": {ctrlKeyPEM, 0o600},
		"kctl.crt":       {kctlCertPEM, 0o644},
		"kctl.key":       {kctlKeyPEM, 0o600},
	}
	for name, f := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(f.pem), f.mode); err != nil {
			return fmt.Errorf("writing %s: %w", path, err)
		}
	}
	return nil
}

func clusterCertsPresent(dir string) bool {
	for _, name := range clusterCertFiles {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			return false
		}
	}
	return true
}

func newCA(cn string, life time.Duration, pathLen int) (*ecdsa.PrivateKey, *x509.Certificate, string, string, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, "", "", err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial(),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(life),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}
	if pathLen >= 0 {
		tmpl.MaxPathLen = pathLen
		tmpl.MaxPathLenZero = pathLen == 0
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, "", "", err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, "", "", err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM, err := marshalKey(key)
	if err != nil {
		return nil, nil, "", "", err
	}
	return key, cert, string(certPEM), keyPEM, nil
}

func signCert(key *ecdsa.PrivateKey, parent *x509.Certificate, parentKey *ecdsa.PrivateKey, cn string, life time.Duration, isCA bool, pathLen int, dns []string, ips []net.IP) (string, error) {
	tmpl := &x509.Certificate{
		SerialNumber:          serial(),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(life),
		IsCA:                  isCA,
		BasicConstraintsValid: true,
		DNSNames:              dns,
		IPAddresses:           ips,
	}
	if isCA {
		tmpl.KeyUsage = x509.KeyUsageCertSign | x509.KeyUsageCRLSign
		tmpl.MaxPathLen = pathLen
		tmpl.MaxPathLenZero = pathLen == 0
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, &key.PublicKey, parentKey)
	if err != nil {
		return "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), nil
}

func signLeaf(key *ecdsa.PrivateKey, parent *x509.Certificate, parentKey *ecdsa.PrivateKey, cn, host string, server, client bool) (string, string, error) {
	var dns []string
	var ips []net.IP
	if host != "" {
		if ip := net.ParseIP(host); ip != nil {
			ips = []net.IP{ip}
		} else {
			dns = []string{host}
		}
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial(),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(leafValidity),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		DNSNames:     dns,
		IPAddresses:  ips,
	}
	if server {
		tmpl.ExtKeyUsage = append(tmpl.ExtKeyUsage, x509.ExtKeyUsageServerAuth)
	}
	if client {
		tmpl.ExtKeyUsage = append(tmpl.ExtKeyUsage, x509.ExtKeyUsageClientAuth)
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, &key.PublicKey, parentKey)
	if err != nil {
		return "", "", err
	}
	keyPEM, err := marshalKey(key)
	if err != nil {
		return "", "", err
	}
	certPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	return certPEM, keyPEM, nil
}

func marshalKey(key *ecdsa.PrivateKey) (string, error) {
	b, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: b})), nil
}

func serial() *big.Int {
	n, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return big.NewInt(time.Now().UnixNano())
	}
	return n
}

func readPEM(dir, name string) (string, error) {
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func loadCA(dir string) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	certPEM, err := readPEM(dir, "ca.crt")
	if err != nil {
		return nil, nil, err
	}
	keyPEM, err := readPEM(dir, "ca.key")
	if err != nil {
		return nil, nil, err
	}
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		return nil, nil, fmt.Errorf("ca.crt is not PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("parse ca.crt: %w", err)
	}
	kblock, _ := pem.Decode([]byte(keyPEM))
	if kblock == nil {
		return nil, nil, fmt.Errorf("ca.key is not PEM")
	}
	keyAny, err := x509.ParsePKCS8PrivateKey(kblock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("parse ca.key: %w", err)
	}
	key, ok := keyAny.(*ecdsa.PrivateKey)
	if !ok {
		return nil, nil, fmt.Errorf("ca.key is not an ECDSA key")
	}
	return cert, key, nil
}

// signHostLeaf signs a fresh leaf with the cluster CA. Matches kctl's
// per-host controller and node certificates (CN kcore-controller-{host}
// or kcore-node-{host}). The on-disk controller.crt is left unchanged.
func signHostLeaf(dir, cn, host string, server, client bool) (string, string, error) {
	ca, caKey, err := loadCA(dir)
	if err != nil {
		return "", "", err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	return signLeaf(key, ca, caKey, cn, host, server, client)
}
