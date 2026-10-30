package store

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
)

// Signer identifies the certificate an APK was signed with. Android only
// installs an update whose signer matches the installed app's, so a version
// signed with a different key than its predecessors cannot be installed over
// them; recording this at upload time surfaces the problem before every TV
// hits it.
type Signer struct {
	SHA256 string `json:"sha256"`         // hex fingerprint of the DER certificate, as apksigner prints it
	Name   string `json:"name,omitempty"` // certificate subject, e.g. "CN=ATV Loader, O=ophymx"
}

// apkSigner returns the first signer of the APK at p, preferring the v3 then
// v2 APK Signing Block and falling back to a v1 (JAR) signature. An unsigned
// APK yields (nil, nil); Android would refuse to install it anyway.
func apkSigner(p string) (*Signer, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	der, err := signingBlockCert(f)
	if err != nil {
		return nil, err
	}
	if der == nil {
		if der, err = jarSignatureCert(p); err != nil {
			return nil, err
		}
	}
	if der == nil {
		return nil, nil
	}
	sum := sha256.Sum256(der)
	s := &Signer{SHA256: hex.EncodeToString(sum[:])}
	if c, err := x509.ParseCertificate(der); err == nil {
		s.Name = c.Subject.String()
	}
	return s, nil
}

func signerLabel(s *Signer) string {
	if s.Name != "" {
		return s.Name
	}
	return "an unnamed certificate"
}

// Block IDs inside the APK Signing Block.
const (
	sigBlockV2 = 0x7109871a
	sigBlockV3 = 0xf05368c0
)

// signingBlockCert finds the APK Signing Block (the bytes just before the
// zip central directory) and returns the first certificate of the first
// signer in the v3 block, or the v2 block if there is no v3. nil when the
// APK has no signing block.
func signingBlockCert(r io.ReadSeeker) ([]byte, error) {
	cdOffset, err := centralDirectoryOffset(r)
	if err != nil {
		return nil, err
	}
	if cdOffset < 32 {
		return nil, nil
	}
	// Footer: uint64 size of block (excluding the leading size field), 16-byte magic.
	var footer [24]byte
	if _, err := r.Seek(cdOffset-24, io.SeekStart); err != nil {
		return nil, err
	}
	if _, err := io.ReadFull(r, footer[:]); err != nil {
		return nil, err
	}
	if string(footer[8:]) != "APK Sig Block 42" {
		return nil, nil
	}
	size := binary.LittleEndian.Uint64(footer[:8])
	if size < 24 || size > 1<<26 || int64(size)+8 > cdOffset {
		return nil, fmt.Errorf("apk signing block: bad size %d", size)
	}
	start := cdOffset - int64(size) - 8
	block := make([]byte, size+8)
	if _, err := r.Seek(start, io.SeekStart); err != nil {
		return nil, err
	}
	if _, err := io.ReadFull(r, block); err != nil {
		return nil, err
	}
	if binary.LittleEndian.Uint64(block[:8]) != size {
		return nil, errors.New("apk signing block: size fields disagree")
	}
	// id-value pairs: uint64 length (of id+value), uint32 id, value.
	pairs := block[8 : len(block)-24]
	var v2, v3 []byte
	for len(pairs) >= 12 {
		n := binary.LittleEndian.Uint64(pairs[:8])
		if n < 4 || n > uint64(len(pairs)-8) {
			return nil, errors.New("apk signing block: bad pair length")
		}
		id := binary.LittleEndian.Uint32(pairs[8:12])
		val := pairs[12 : 8+n]
		switch id {
		case sigBlockV3:
			v3 = val
		case sigBlockV2:
			v2 = val
		}
		pairs = pairs[8+n:]
	}
	for _, blk := range [][]byte{v3, v2} {
		if blk == nil {
			continue
		}
		der, err := firstCertInSignerBlock(blk)
		if err != nil {
			return nil, err
		}
		if der != nil {
			return der, nil
		}
	}
	return nil, nil
}

// firstCertInSignerBlock walks the v2/v3 structure far enough to reach the
// certificates: signers (lp) → signer (lp) → signed data (lp) → digests (lp),
// certificates (lp) → certificate (lp). lp = uint32 little-endian length prefix.
func firstCertInSignerBlock(b []byte) ([]byte, error) {
	signers, _, err := lpTake(b)
	if err != nil {
		return nil, err
	}
	if len(signers) == 0 {
		return nil, nil
	}
	signer, _, err := lpTake(signers)
	if err != nil {
		return nil, err
	}
	signedData, _, err := lpTake(signer)
	if err != nil {
		return nil, err
	}
	_, rest, err := lpTake(signedData) // digests
	if err != nil {
		return nil, err
	}
	certs, _, err := lpTake(rest)
	if err != nil {
		return nil, err
	}
	if len(certs) == 0 {
		return nil, nil
	}
	cert, _, err := lpTake(certs)
	return cert, err
}

func lpTake(b []byte) (val, rest []byte, err error) {
	if len(b) < 4 {
		return nil, nil, errors.New("apk signing block: truncated")
	}
	n := binary.LittleEndian.Uint32(b[:4])
	if int(n) > len(b)-4 {
		return nil, nil, errors.New("apk signing block: length past end")
	}
	return b[4 : 4+n], b[4+n:], nil
}

// centralDirectoryOffset locates the end-of-central-directory record and
// returns where the central directory starts, following the zip64 locator
// when the 32-bit field is saturated.
func centralDirectoryOffset(r io.ReadSeeker) (int64, error) {
	end, err := r.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, err
	}
	const maxComment = 65535 + 22
	n := int64(maxComment)
	if n > end {
		n = end
	}
	buf := make([]byte, n)
	if _, err := r.Seek(end-n, io.SeekStart); err != nil {
		return 0, err
	}
	if _, err := io.ReadFull(r, buf); err != nil {
		return 0, err
	}
	sig := []byte{0x50, 0x4b, 0x05, 0x06}
	i := bytes.LastIndex(buf, sig)
	for i >= 0 {
		// The comment length must reach exactly to the end of the file.
		if i+22 <= len(buf) && int(binary.LittleEndian.Uint16(buf[i+20:]))+i+22 == len(buf) {
			break
		}
		i = bytes.LastIndex(buf[:i], sig)
	}
	if i < 0 {
		return 0, errors.New("zip: end of central directory not found")
	}
	eocd := end - n + int64(i)
	off := int64(binary.LittleEndian.Uint32(buf[i+16:]))
	if off != 0xFFFFFFFF {
		return off, nil
	}
	// zip64: locator sits right before the EOCD and points at the zip64 EOCD record.
	var loc [20]byte
	if _, err := r.Seek(eocd-20, io.SeekStart); err != nil {
		return 0, err
	}
	if _, err := io.ReadFull(r, loc[:]); err != nil {
		return 0, err
	}
	if binary.LittleEndian.Uint32(loc[:4]) != 0x07064b50 {
		return 0, errors.New("zip64: locator not found")
	}
	var rec [56]byte
	if _, err := r.Seek(int64(binary.LittleEndian.Uint64(loc[8:16])), io.SeekStart); err != nil {
		return 0, err
	}
	if _, err := io.ReadFull(r, rec[:]); err != nil {
		return 0, err
	}
	if binary.LittleEndian.Uint32(rec[:4]) != 0x06064b50 {
		return 0, errors.New("zip64: end of central directory record not found")
	}
	return int64(binary.LittleEndian.Uint64(rec[48:56])), nil
}

// jarSignatureCert returns the first certificate from a v1 signature file
// (META-INF/*.RSA|DSA|EC, a PKCS#7 SignedData), or nil when there is none.
func jarSignatureCert(p string) ([]byte, error) {
	z, err := zip.OpenReader(p)
	if err != nil {
		return nil, err
	}
	defer z.Close()
	for _, f := range z.File {
		dir, name := path.Split(f.Name)
		if dir != "META-INF/" {
			continue
		}
		switch strings.ToUpper(path.Ext(name)) {
		case ".RSA", ".DSA", ".EC":
		default:
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(io.LimitReader(rc, 1<<20))
		rc.Close()
		if err != nil {
			return nil, err
		}
		return pkcs7FirstCert(data)
	}
	return nil, nil
}

// pkcs7FirstCert digs the first certificate out of a DER PKCS#7 SignedData:
// ContentInfo{oid, [0] SignedData{version, digestAlgs, contentInfo, [0] certs, ...}}.
func pkcs7FirstCert(der []byte) ([]byte, error) {
	var ci struct {
		OID     asn1.ObjectIdentifier
		Content asn1.RawValue `asn1:"explicit,tag:0"`
	}
	if _, err := asn1.Unmarshal(der, &ci); err != nil {
		return nil, fmt.Errorf("pkcs7: %w", err)
	}
	var sd struct {
		Version    int
		DigestAlgs asn1.RawValue
		Content    asn1.RawValue
		Certs      asn1.RawValue `asn1:"optional,tag:0"`
	}
	if _, err := asn1.Unmarshal(ci.Content.Bytes, &sd); err != nil {
		return nil, fmt.Errorf("pkcs7 signed data: %w", err)
	}
	if len(sd.Certs.Bytes) == 0 {
		return nil, nil
	}
	var first asn1.RawValue
	if _, err := asn1.Unmarshal(sd.Certs.Bytes, &first); err != nil {
		return nil, fmt.Errorf("pkcs7 certificate: %w", err)
	}
	return first.FullBytes, nil
}
