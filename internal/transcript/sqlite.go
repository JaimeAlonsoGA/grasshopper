package transcript

import (
	"context"
	"database/sql"
	"time"

	_ "modernc.org/sqlite"
)

// Two agents keep every conversation in one database instead of one file each,
// and they are not the last: an editor that already has a store puts its chat in
// it. Opening that store is the same job every time, and the way it is opened is
// the part with a rule attached, so it lives here rather than in whichever
// reader needed it first.

// openRead opens somebody else's database without touching it.
//
// Read-only and immutable are not the same promise and both are wanted. Read-only
// says grasshopper will not write. Immutable says it will not create the journal
// and write-ahead files a reader otherwise leaves beside a database — which would
// be writing into another app's state through the back door, and would fail
// outright while the app holds the lock.
func openRead(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro&immutable=1&_pragma=query_only(1)")
	if err != nil {
		return nil, err
	}
	// One connection: a listing walks a handful of rows and a capture reads one
	// conversation. A pool would open the same immutable file several times for
	// no gain.
	db.SetMaxOpenConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), openTimeout)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// openTimeout bounds the wait for a database that is not going to answer — a
// file on a disconnected volume, or one an agent is holding in a way this cannot
// share. A listing must not hang on it.
const openTimeout = 5 * time.Second

// seconds normalises a stamp that may arrive in seconds or milliseconds. Which
// of the two a store used is not something a reader should have to remember.
func seconds(v int64) int64 {
	if v > 1e12 {
		return v / 1000
	}
	return v
}
