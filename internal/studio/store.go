package studio

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const FileName = "studio.sqlite3"

type Store struct {
	mu   sync.Mutex
	db   *nativeDB
	Root string
}

func ID() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b[:])
}
func num(n int64) string { return strconv.FormatInt(n, 10) }
func integer(s string) int64 {
	n, e := strconv.ParseInt(s, 10, 64)
	if e != nil {
		panic("invalid SQLite integer: " + s)
	}
	return n
}
func Open(root string) (*Store, error) {
	root, e := filepath.Abs(root)
	if e != nil {
		return nil, e
	}
	if e = os.MkdirAll(root, 0700); e != nil {
		return nil, e
	}
	if st, e := os.Lstat(root); e != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("invalid data directory")
	}
	p := filepath.Join(root, FileName)
	if st, e := os.Lstat(p); e == nil && (!st.Mode().IsRegular() || st.Mode()&os.ModeSymlink != 0) {
		return nil, errors.New("invalid SQLite path")
	}
	d, e := openNative(p, false)
	if e != nil {
		return nil, e
	}
	s := &Store{db: d, Root: root}
	for _, q := range []string{"PRAGMA busy_timeout=2000", "PRAGMA journal_mode=WAL", "PRAGMA synchronous=FULL", "PRAGMA foreign_keys=ON", "PRAGMA quick_check"} {
		r, _, e := d.query(q)
		if e != nil {
			s.Close()
			return nil, e
		}
		if q == "PRAGMA journal_mode=WAL" && (len(r) != 1 || r[0][0] != "wal") {
			s.Close()
			return nil, errors.New("SQLite WAL mode unavailable; no work started")
		}
		if q == "PRAGMA quick_check" && (len(r) != 1 || r[0][0] != "ok") {
			s.Close()
			return nil, errors.New("SQLite integrity check failed")
		}
	}
	e = s.tx(func(d *nativeDB) error {
		for _, q := range schema {
			if _, _, e := d.query(q); e != nil {
				return e
			}
		}
		r, _, e := d.query("SELECT version FROM schema_info")
		if e != nil {
			return e
		}
		if len(r) != 1 || r[0][0] != "1" {
			return errors.New("unsupported studio database schema; original retained")
		}
		return nil
	})
	if e != nil {
		s.Close()
		return nil, e
	}
	_ = os.Chmod(p, 0600)
	return s, nil
}
func VerifyFile(root string) error {
	p := filepath.Join(root, FileName)
	st, e := os.Lstat(p)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	if !st.Mode().IsRegular() || st.Mode()&os.ModeSymlink != 0 {
		return errors.New("invalid SQLite path")
	}
	d, e := openNative(p, true)
	if e != nil {
		return e
	}
	defer d.close()
	r, _, e := d.query("PRAGMA quick_check")
	if e != nil {
		return e
	}
	if len(r) != 1 || r[0][0] != "ok" {
		return errors.New("SQLite quick_check failed")
	}
	r, _, e = d.query("SELECT version FROM schema_info")
	if e != nil {
		return e
	}
	if len(r) != 1 || r[0][0] != "1" {
		return errors.New("unsupported studio schema")
	}
	return nil
}
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil
	}
	_, _, e := s.db.query("PRAGMA wal_checkpoint(TRUNCATE)")
	e = errors.Join(e, s.db.close())
	s.db = nil
	return e
}
func (s *Store) tx(f func(*nativeDB) error) (err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return errors.New("database closed")
	}
	d := s.db
	if _, _, err = d.query("BEGIN IMMEDIATE"); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_, _, _ = d.query("ROLLBACK")
		}
	}()
	if err = f(d); err != nil {
		return err
	}
	_, _, err = d.query("COMMIT")
	committed = err == nil
	return err
}
func (s *Store) read(q string, args ...string) ([][]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, errors.New("database closed")
	}
	r, _, e := s.db.query(q, args...)
	return r, e
}
func validText(s string) bool { return len(s) > 0 && len(s) <= 256 && !strings.ContainsRune(s, 0) }
func hashOK(s string) bool    { b, e := hex.DecodeString(s); return e == nil && len(b) == 32 }
func Day(t time.Time) string  { return t.In(time.FixedZone("CST", 8*3600)).Format("2006-01-02") }
func Weekly(t time.Time) string {
	y, w := t.In(time.FixedZone("CST", 8*3600)).ISOWeek()
	return fmt.Sprintf("%04d-W%02d", y, w)
}

var schema = []string{
	"CREATE TABLE IF NOT EXISTS schema_info(version INTEGER NOT NULL)",
	"INSERT INTO schema_info(version) SELECT 1 WHERE NOT EXISTS(SELECT 1 FROM schema_info)",
	"CREATE TABLE IF NOT EXISTS plans(id TEXT PRIMARY KEY,day TEXT NOT NULL,config TEXT NOT NULL,created INTEGER NOT NULL)",
	"CREATE TABLE IF NOT EXISTS tasks(plan TEXT NOT NULL REFERENCES plans(id),phase INTEGER NOT NULL,actor TEXT NOT NULL,module TEXT NOT NULL,state TEXT NOT NULL DEFAULT 'PENDING' CHECK(state IN ('PENDING','RUNNING','VERIFIED_DONE','CHECKED_SKIP','FAILED','NEEDS_REVIEW')),attempt INTEGER NOT NULL DEFAULT 0,evidence TEXT NOT NULL DEFAULT '',reason TEXT NOT NULL DEFAULT '',updated INTEGER NOT NULL,PRIMARY KEY(plan,phase,actor))",
	"CREATE TABLE IF NOT EXISTS budgets(actor TEXT PRIMARY KEY,silver INTEGER NOT NULL CHECK(silver>=0),merit INTEGER NOT NULL CHECK(merit>=0),capacity INTEGER NOT NULL CHECK(capacity>=0),epoch TEXT NOT NULL,evidence TEXT NOT NULL,observed INTEGER NOT NULL)",
	"CREATE TABLE IF NOT EXISTS orders(id TEXT PRIMARY KEY,buyer TEXT NOT NULL,total INTEGER NOT NULL CHECK(total>0 AND total%10000=0),created INTEGER NOT NULL,completed INTEGER NOT NULL DEFAULT 0,enabled INTEGER NOT NULL DEFAULT 0 CHECK(enabled IN (0,1)))",
	"CREATE TABLE IF NOT EXISTS transfers(id TEXT PRIMARY KEY,request TEXT NOT NULL UNIQUE,order_id TEXT NOT NULL REFERENCES orders(id),actor TEXT NOT NULL REFERENCES budgets(actor),epoch TEXT NOT NULL,amount INTEGER NOT NULL CHECK(amount>0 AND amount%10000=0),requested INTEGER NOT NULL,state TEXT NOT NULL CHECK(state IN ('RESERVED','DISPATCHED','NEEDS_REVIEW','CONFIRMED','CANCELLED')),receipt TEXT NOT NULL DEFAULT '',proof TEXT NOT NULL DEFAULT '',proof_sha TEXT NOT NULL DEFAULT '',updated INTEGER NOT NULL)",
	"CREATE TABLE IF NOT EXISTS outbox(seq INTEGER PRIMARY KEY AUTOINCREMENT,transfer_id TEXT NOT NULL UNIQUE REFERENCES transfers(id),message TEXT NOT NULL,delivered INTEGER NOT NULL DEFAULT 0)",
	"CREATE TABLE IF NOT EXISTS events(seq INTEGER PRIMARY KEY AUTOINCREMENT,kind TEXT NOT NULL,object TEXT NOT NULL,detail TEXT NOT NULL,created INTEGER NOT NULL)",
}
