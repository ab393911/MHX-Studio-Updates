package studio

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func safeID(v string) bool { b, e := hex.DecodeString(v); return e == nil && len(b) == 16 }
func (s *Store) proofDir(buyer, order string) (string, error) {
	if !validText(buyer) || !safeID(order) {
		return "", errors.New("invalid proof identity")
	}
	hash := sha256.Sum256([]byte(buyer))
	p := filepath.Join(s.Root, "proofs", hex.EncodeToString(hash[:16]), order)
	if e := safePath(s.Root, p); e != nil {
		return "", e
	}
	return p, nil
}
func safePath(root, path string) error {
	rel, e := filepath.Rel(root, path)
	if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return errors.New("proof path escapes root")
	}
	p := root
	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		if part == "" || part == "." || part == ".." {
			return errors.New("invalid proof path")
		}
		p = filepath.Join(p, part)
		st, e := os.Lstat(p)
		if e == nil && st.Mode()&os.ModeSymlink != 0 {
			return errors.New("proof symlink rejected")
		}
		if e != nil && !os.IsNotExist(e) {
			return e
		}
	}
	return nil
}
func (s *Store) SaveProof(buyer, order, tx string, frame []byte, at time.Time) (path, hash string, err error) {
	if !safeID(tx) || at.IsZero() || len(frame) > 8<<20 {
		return "", "", errors.New("invalid proof")
	}
	c, e := png.DecodeConfig(bytes.NewReader(frame))
	if e != nil || c.Width < 1 || c.Height < 1 || c.Width > 4096 || c.Height > 4096 {
		return "", "", errors.New("invalid proof PNG")
	}
	dir, e := s.proofDir(buyer, order)
	if e != nil {
		return "", "", e
	}
	path = filepath.Join(dir, tx+"_"+at.UTC().Format("20060102T150405.000000000Z")+".png")
	if e = safePath(s.Root, path); e != nil {
		return "", "", e
	}
	if e = os.MkdirAll(dir, 0700); e != nil {
		return "", "", e
	}
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return "", "", e
	}
	_, e = f.Write(frame)
	if e == nil {
		e = f.Sync()
	}
	e = errors.Join(e, f.Close())
	if e != nil {
		return "", "", e
	}
	h := sha256.Sum256(frame)
	return path, hex.EncodeToString(h[:]), nil
}
func (s *Store) verifyProof(buyer, order, tx, path, hash string) error {
	dir, e := s.proofDir(buyer, order)
	if e != nil {
		return e
	}
	if filepath.Dir(path) != dir || !strings.HasPrefix(filepath.Base(path), tx+"_") || filepath.Ext(path) != ".png" {
		return errors.New("proof does not belong to buyer/order/transaction")
	}
	if e = safePath(s.Root, path); e != nil {
		return e
	}
	st, e := os.Lstat(path)
	if e != nil {
		return e
	}
	if !st.Mode().IsRegular() || st.Size() > 8<<20 {
		return errors.New("invalid proof file")
	}
	b, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	h := sha256.Sum256(b)
	if hex.EncodeToString(h[:]) != hash {
		return errors.New("proof hash mismatch")
	}
	_, e = png.DecodeConfig(bytes.NewReader(b))
	return e
}

// Only completed orders older than ten full days are eligible. The ledger,
// receipts and unfinished/uncertain orders are never deleted by this routine.
func (s *Store) CleanupProofs(now time.Time) (int, error) {
	count := 0
	e := s.tx(func(d *nativeDB) error {
		rows, _, e := d.query("SELECT id,buyer FROM orders WHERE completed>0 AND completed<=?", num(now.Add(-10*24*time.Hour).Unix()))
		if e != nil {
			return e
		}
		for _, r := range rows {
			o, e := orderAt(d, r[0])
			if e != nil {
				return e
			}
			if o.Confirmed != o.Total || o.Held != 0 {
				continue
			}
			dir, e := s.proofDir(r[1], r[0])
			if e != nil {
				return e
			}
			if _, e = os.Lstat(dir); os.IsNotExist(e) {
				continue
			} else if e != nil {
				return e
			}
			e = filepath.WalkDir(dir, func(path string, de os.DirEntry, e error) error {
				if e != nil {
					return e
				}
				return safePath(s.Root, path)
			})
			if e != nil {
				return e
			}
			if e = os.RemoveAll(dir); e != nil {
				return e
			}
			count++
			if _, _, e = d.query("INSERT INTO events(kind,object,detail,created) VALUES('proof_cleanup',?,'completed over ten days; ledger retained',?)", r[0], num(now.Unix())); e != nil {
				return e
			}
		}
		return nil
	})
	return count, e
}
