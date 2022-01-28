package strategy

import (
	"bytes"
	"io"

	"github.com/PowerDNS/lmdb-go/lmdb"
	"github.com/pkg/errors"
)

const LMDBMaxKeySize = 511

// iterBothFunc is the callback called by iterBoth.
// Here 'db' refers to LMDB and 'it' to the Iterator with data we want to insert.
// It is called with the key of the side that's behind, or both if they are equal.
// The EOF flags indicate if a side is finished. There are never both true.
type iterBothFunc func(itKey, dbKey, dbVal []byte, itEOF, dbEOF bool) error

// iterBoth iterates over both LMDB and the Iterator and calls the callback
// function with the values.
func iterBoth(it Iterator, c *lmdb.Cursor, f iterBothFunc) error {
	itEOF := false
	dbEOF := false
	var itKey, dbKey, dbVal []byte
	var err error
	prevKey := make([]byte, 0, LMDBMaxKeySize)

	var flag uint = lmdb.First
	for {
		// Next iterator key if needed
		if itKey == nil && !itEOF {
			itKey, err = it.Next()
			if err != nil {
				if err == io.EOF {
					itKey = nil
					itEOF = true
				} else {
					return errors.Wrap(err, "iterator next")
				}
			} else {
				// Check to ensure the keys are in insert order
				if bytes.Compare(prevKey, itKey) >= 0 {
					return errors.Wrap(ErrNotSorted, string(itKey))
				}
				prevKey = prevKey[:len(itKey)]
				copy(prevKey, itKey)
			}
			//log.Printf("@@@ < IT %s", string(itKey))
		}

		// Next LMDB key if needed
		if dbKey == nil && !dbEOF {
			dbKey, dbVal, err = c.Get(nil, nil, flag)
			if err != nil {
				if lmdb.IsNotFound(err) {
					dbEOF = true
				} else {
					return errors.Wrap(err, "cursor next")
				}
			}
			flag = lmdb.Next
			//log.Printf("@@@ < DB %s (val: %s)", string(dbKey), string(dbVal))
		}

		// No need for compare if we reached the end of one
		if itEOF && dbEOF {
			return nil // done
		}
		if itEOF {
			err = f(nil, dbKey, dbVal, true, false)
			dbKey = nil
			if err != nil {
				return errors.Wrap(err, "callback it eof")
			}
			continue
		}
		if dbEOF {
			err = f(itKey, nil, nil, false, true)
			itKey = nil
			if err != nil {
				return errors.Wrap(err, "callback db eof")
			}
			continue
		}

		// Compare
		cmp := bytes.Compare(dbKey, itKey)
		if cmp < 0 {
			// LMDB key is smaller
			err = f(nil, dbKey, dbVal, false, false)
			dbKey = nil
		} else if cmp == 0 {
			// Same key
			err = f(itKey, dbKey, dbVal, false, false)
			dbKey = nil
			itKey = nil
		} else {
			// We just passed the iterator key
			err = f(itKey, nil, nil, false, false)
			itKey = nil
		}
		if err != nil {
			return errors.Wrap(err, "callback")
		}
	}
}
