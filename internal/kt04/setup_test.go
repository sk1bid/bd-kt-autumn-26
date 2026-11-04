package kt04

import (
	"crypto/tls"
	"crypto/x509"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupPreservesCredentialsAndIssuesValidServerCertificates(t *testing.T) {
	root := t.TempDir()
	secrets := filepath.Join(t.TempDir(), "secrets")
	t.Setenv("KT04_SECRETS_DIR", secrets)
	if err := Setup(root); err != nil {
		t.Fatal(err)
	}
	read := func(name string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(secrets, name))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(read("ca.crt")) {
		t.Fatal("could not parse CA certificate")
	}
	for _, node := range secretNodes {
		pair, err := tls.X509KeyPair(read(node.name+".crt"), read(node.name+".key"))
		if err != nil {
			t.Fatal(err)
		}
		cert, err := x509.ParseCertificate(pair.Certificate[0])
		if err != nil {
			t.Fatal(err)
		}
		for _, host := range []string{node.name, "localhost", "127.0.0.1"} {
			if _, err := cert.Verify(x509.VerifyOptions{DNSName: host, Roots: roots}); err != nil {
				t.Errorf("certificate for %s failed verification as %s: %v", node.name, host, err)
			}
		}
	}

	password := strings.TrimSpace(string(read("kt04_replicator.password")))
	if got := string(read("kt04_replicator.pgpass")); got != "primary:5432:replication:kt04_replicator:"+password+"\n" {
		t.Fatal("replication passfile does not match its password")
	}
	if err := Setup(root); err != nil {
		t.Fatal(err)
	}
	if password != strings.TrimSpace(string(read("kt04_replicator.password"))) {
		t.Fatal("setup replaced an existing credential")
	}

	t.Setenv("KT04_SECRETS_DIR", filepath.Join(root, "new", "secrets"))
	if err := Setup(root); err == nil {
		t.Fatal("setup accepted a destination inside the repository")
	}
}
