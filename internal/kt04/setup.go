package kt04

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const defaultSecretsDir = "/private/tmp/transport-kt04-secrets"

var secretNodes = []struct {
	name string
	port int
}{
	{"primary", 55404},
	{"replica", 55405},
	{"restore", 55406},
	{"pitr", 55407},
}

var secretRoles = []string{
	"postgres",
	"kt04_admin",
	"kt04_app",
	"kt04_analyst",
	"kt04_driver1",
	"kt04_driver2",
	"kt04_replicator",
}

// Setup prepares the external credentials directory or verifies its existing files.
func Setup(root string) error {
	directory := os.Getenv("KT04_SECRETS_DIR")
	if directory == "" {
		directory = defaultSecretsDir
	}
	if !filepath.IsAbs(directory) {
		return errors.New("KT04_SECRETS_DIR must be an absolute path")
	}
	directory = filepath.Clean(directory)
	repository, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	repository, err = filepath.Abs(repository)
	if err != nil {
		return err
	}
	resolved, err := resolveSetupPath(directory)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(repository, resolved)
	if err != nil {
		return err
	}
	if relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errors.New("KT04_SECRETS_DIR must be outside the repository")
	}

	if _, err := os.Lstat(directory); !errors.Is(err, os.ErrNotExist) {
		if err != nil {
			return err
		}
		return verifySecretsDirectory(directory)
	}

	parent := filepath.Dir(directory)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(parent, ".kt04-credentials-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	if err := createTLSFiles(staging); err != nil {
		return err
	}
	if err := createPasswordFiles(staging); err != nil {
		return err
	}
	if err := verifySecretsDirectory(staging); err != nil {
		return err
	}
	if err := os.Rename(staging, directory); err != nil {
		// Another setup invocation may have published a complete set meanwhile.
		if verifySecretsDirectory(directory) == nil {
			return nil
		}
		return err
	}
	return nil
}

// resolveSetupPath resolves symlinks even when the leaf or a parent is absent.
func resolveSetupPath(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if !errors.Is(err, os.ErrNotExist) {
		return resolved, err
	}
	parent := filepath.Dir(path)
	if parent == path {
		return "", err
	}
	resolved, err = resolveSetupPath(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(resolved, filepath.Base(path)), nil
}

func verifySecretsDirectory(directory string) error {
	info, err := os.Lstat(directory)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm() != 0700 || !ownedByCurrentUser(info) {
		return fmt.Errorf("%s must be a directory owned by the current user with mode 0700", directory)
	}
	names := []string{"ca.crt", "pgpass"}
	for _, node := range secretNodes {
		names = append(names, node.name+".crt", node.name+".key")
	}
	for _, role := range secretRoles {
		names = append(names, role+".password", role+".pgpass")
	}
	for _, name := range names {
		info, err := os.Lstat(filepath.Join(directory, name))
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() == 0 || !ownedByCurrentUser(info) {
			return fmt.Errorf("%s must be a nonempty regular file owned by the current user with mode 0600", name)
		}
	}
	return nil
}

func ownedByCurrentUser(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Getuid())
}

func createPasswordFiles(directory string) error {
	var combined strings.Builder
	for _, role := range secretRoles {
		var entropy [32]byte
		if _, err := io.ReadFull(rand.Reader, entropy[:]); err != nil {
			return err
		}
		password := hex.EncodeToString(entropy[:])
		if err := os.WriteFile(filepath.Join(directory, role+".password"), []byte(password+"\n"), 0600); err != nil {
			return err
		}
		var passfile strings.Builder
		if role == "kt04_replicator" {
			fmt.Fprintf(&passfile, "primary:5432:replication:%s:%s\n", role, password)
		} else {
			for _, node := range secretNodes {
				fmt.Fprintf(&passfile, "%s:5432:*:%s:%s\n", node.name, role, password)
				fmt.Fprintf(&passfile, "localhost:%d:*:%s:%s\n", node.port, role, password)
				fmt.Fprintf(&passfile, "127.0.0.1:%d:*:%s:%s\n", node.port, role, password)
			}
		}
		combined.WriteString(passfile.String())
		if err := os.WriteFile(filepath.Join(directory, role+".pgpass"), []byte(passfile.String()), 0600); err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(directory, "pgpass"), []byte(combined.String()), 0600)
}

func createTLSFiles(directory string) error {
	now := time.Now().UTC()
	authority, err := certificateTemplate("Transport KT04 local CA", now)
	if err != nil {
		return err
	}
	authority.IsCA = true
	authority.BasicConstraintsValid = true
	authority.MaxPathLenZero = true
	authority.KeyUsage = x509.KeyUsageCertSign | x509.KeyUsageCRLSign
	caKey, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		return err
	}
	if err := writeCertificate(directory, "ca", authority, authority, caKey, caKey); err != nil {
		return err
	}

	for _, node := range secretNodes {
		server, err := certificateTemplate(node.name, now)
		if err != nil {
			return err
		}
		server.BasicConstraintsValid = true
		server.KeyUsage = x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment
		server.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		server.DNSNames = []string{node.name, "localhost"}
		server.IPAddresses = []net.IP{net.IPv4(127, 0, 0, 1)}
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			return err
		}
		if err := writeCertificate(directory, node.name, server, authority, key, caKey); err != nil {
			return err
		}
		keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
		if err := os.WriteFile(filepath.Join(directory, node.name+".key"), keyPEM, 0600); err != nil {
			return err
		}
	}
	return nil
}

func certificateTemplate(name string, now time.Time) (*x509.Certificate, error) {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}
	return &x509.Certificate{
		SerialNumber: serial.Add(serial, big.NewInt(1)),
		Subject:      pkix.Name{CommonName: name},
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     now.AddDate(1, 0, 0),
	}, nil
}

func writeCertificate(directory, name string, template, issuer *x509.Certificate, key, signer *rsa.PrivateKey) error {
	certificate, err := x509.CreateCertificate(rand.Reader, template, issuer, &key.PublicKey, signer)
	if err != nil {
		return err
	}
	content := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate})
	return os.WriteFile(filepath.Join(directory, name+".crt"), content, 0600)
}
