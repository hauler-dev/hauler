package cache

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/opencontainers/go-digest"
	"go.etcd.io/bbolt"
)

const (
	ContentBucketName = "data"
	ContentBlobsDir   = "blobs"
	RefBucketName     = "refs"
)

type Cache interface {
	Add(string, []byte) error
}

type BoltDB struct {
	dir  string
	file string
	db   *bbolt.DB
}

func NewBoltDB(path string, name string) (*BoltDB, error) {
	if err := os.MkdirAll(path, os.ModePerm); err != nil {
		return nil, err
	}

	blobsDir := filepath.Join(path, ContentBlobsDir)
	if err := os.MkdirAll(blobsDir, os.ModePerm); err != nil {
		return nil, err
	}

	dbfile := filepath.Join(path, fmt.Sprintf("%s.db", name))
	db, err := bbolt.Open(dbfile, 0666, &bbolt.Options{
		Timeout: 10 * time.Second,
	})
	if err != nil {
		return nil, err
	}

	if err := db.Close(); err != nil {
		return nil, err
	}

	return &BoltDB{
		dir:  path,
		file: dbfile,
		db:   db,
	}, nil
}

func (s *BoltDB) Add(ref string, data []byte) error {
	db, err := s.open()
	if err != nil {
		return err
	}
	defer db.Close()

	d := digest.FromBytes(data)

	err = db.Update(func(tx *bbolt.Tx) error {
		// Put reference to link
		rb, err := tx.CreateBucketIfNotExists([]byte(RefBucketName))
		if err != nil {
			return err
		}

		if err := rb.Put([]byte(ref), []byte(d.String())); err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return err
	}

	// Put data as a blob on the filesystem since everything is content addressable
	if err := s.writeBlob(d.String(), data); err != nil {
		return err
	}

	return nil
}

func (s *BoltDB) bucket() []byte {
	return []byte(ContentBucketName)
}

func (s *BoltDB) open() (*bbolt.DB, error) {
	return bbolt.Open(s.file, os.ModePerm, &bbolt.Options{})
}

func (s *BoltDB) writeBlob(digest string, data []byte) error {
	fp := filepath.Join(s.dir, ContentBlobsDir, digest)
	return os.WriteFile(fp, data, os.ModePerm)
}
