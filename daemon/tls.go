package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"time"

	"github.com/spf13/viper"
)

func pemBlockForKey(priv *rsa.PrivateKey) *pem.Block {
	return &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)}
}

func CreateSelfSignedCertificate(host string) (*rsa.PrivateKey, []byte, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 4096)
	notBefore := time.Now()

	notAfter := notBefore.Add(time.Duration(viper.GetInt("CertValidity")) * 24 * time.Hour)

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		//log.Fatalf("failed to generate serial number: %s", err)
		return nil, nil, err
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization:       []string{viper.GetString("CertOrganization")},
			OrganizationalUnit: []string{viper.GetString("CertOrganizationalUnit")},
			Locality:           []string{viper.GetString("CertLocality")},
			Province:           []string{viper.GetString("CertProvince")},
			Country:            []string{viper.GetString("CertCountry")},
		},
		NotBefore: notBefore,
		NotAfter:  notAfter,

		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	template.DNSNames = append(template.DNSNames, host)

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		//log.Fatalf("Failed to create certificate: %s", err)
		return nil, nil, err
	}
	return priv, derBytes, nil
}

func WriteCertificateToFile(certificate []byte, filePath string) error {
	certOut, err := os.Create(filePath)
	if err != nil {
		return err
	}
	pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certificate})
	certOut.Close()
	return nil
}

func WritePrivateKeyToFile(key *rsa.PrivateKey, filePath string) error {
	keyOut, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	pem.Encode(keyOut, pemBlockForKey(key))
	keyOut.Close()
	return nil
}
