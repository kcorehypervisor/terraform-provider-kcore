package provider

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateClusterPKI(t *testing.T) {
	dir := t.TempDir()
	if err := generateClusterPKI(dir, "10.0.0.8:9090"); err != nil {
		t.Fatal(err)
	}
	if !clusterCertsPresent(dir) {
		t.Fatal("expected the eight cluster certificate files")
	}
	ctrl := parseCertFile(t, filepath.Join(dir, "controller.crt"))
	if ctrl.Subject.CommonName != "kcore-controller-10.0.0.8" {
		t.Fatalf("controller CN = %q", ctrl.Subject.CommonName)
	}
	if len(ctrl.IPAddresses) != 1 || ctrl.IPAddresses[0].String() != "10.0.0.8" {
		t.Fatalf("controller SAN = %v", ctrl.IPAddresses)
	}
	kctl := parseCertFile(t, filepath.Join(dir, "kctl.crt"))
	if kctl.Subject.CommonName != "kctl" {
		t.Fatalf("kctl CN = %q", kctl.Subject.CommonName)
	}
	sub := parseCertFile(t, filepath.Join(dir, "sub-ca.crt"))
	if sub.Subject.CommonName != "kcore-cluster-sub-ca" || !sub.IsCA || !sub.MaxPathLenZero {
		t.Fatalf("sub-CA constraints: cn=%s isCA=%v pathLenZero=%v", sub.Subject.CommonName, sub.IsCA, sub.MaxPathLenZero)
	}
	ca := parseCertFile(t, filepath.Join(dir, "ca.crt"))
	if ca.Subject.CommonName != "kcore-cluster-ca" || !ca.IsCA {
		t.Fatalf("root CA: cn=%s isCA=%v", ca.Subject.CommonName, ca.IsCA)
	}
	if err := sub.CheckSignatureFrom(ca); err != nil {
		t.Fatalf("sub-CA not signed by root: %v", err)
	}
	if err := ctrl.CheckSignatureFrom(ca); err != nil {
		t.Fatalf("controller cert not signed by root: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, "ca.key"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("ca.key mode = %o", info.Mode().Perm())
	}
}

func TestSignHostLeafUsesNodeHost(t *testing.T) {
	dir := t.TempDir()
	if err := generateClusterPKI(dir, "controller.example:9090"); err != nil {
		t.Fatal(err)
	}
	certPEM, _, err := signHostLeaf(dir, "kcore-controller-node-a", "node-a", true, true)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		t.Fatal("leaf pem")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if cert.Subject.CommonName != "kcore-controller-node-a" {
		t.Fatalf("CN = %q", cert.Subject.CommonName)
	}
	if len(cert.DNSNames) != 1 || cert.DNSNames[0] != "node-a" {
		t.Fatalf("SAN = %v", cert.DNSNames)
	}
	onDisk := parseCertFile(t, filepath.Join(dir, "controller.crt"))
	if onDisk.Subject.CommonName != "kcore-controller-controller.example" {
		t.Fatalf("on-disk controller cert was overwritten: %s", onDisk.Subject.CommonName)
	}
}

func parseCertFile(t *testing.T, path string) *x509.Certificate {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(b)
	if block == nil {
		t.Fatalf("%s is not PEM", path)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}
