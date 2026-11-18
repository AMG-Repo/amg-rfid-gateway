package localstore

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"github.com/amg-rfid/amg-rfid-gateway/internal/cache"
	"modernc.org/sqlite"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type requeueRaceGateDriver struct {
	base   driver.Driver
	read   chan struct{}
	resume chan struct{}
	once   sync.Once
}

func (d *requeueRaceGateDriver) Open(name string) (driver.Conn, error) {
	c, e := d.base.Open(name)
	if e != nil {
		return nil, e
	}
	return &requeueRaceGateConn{Conn: c, d: d}, nil
}

type requeueRaceGateConn struct {
	driver.Conn
	d *requeueRaceGateDriver
}

func (c *requeueRaceGateConn) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	return c.Conn.(driver.ExecerContext).ExecContext(ctx, q, args)
}
func (c *requeueRaceGateConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	r, e := c.Conn.(driver.QueryerContext).QueryContext(ctx, q, args)
	if e != nil {
		return nil, e
	}
	if strings.Contains(q, "SELECT retry_count FROM pending_confirmations WHERE id = ?") {
		return &requeueRaceGateRows{Rows: r, d: c.d}, nil
	}
	return r, nil
}

type requeueRaceGateRows struct {
	driver.Rows
	d *requeueRaceGateDriver
}

func (r *requeueRaceGateRows) Next(v []driver.Value) error {
	e := r.Rows.Next(v)
	if e == nil {
		r.d.once.Do(func() { close(r.d.read); <-r.d.resume })
	}
	return e
}
func TestScheduleConfirmationRetry_DoesNotOverwriteConcurrentRequeue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "race.db")
	c, e := cache.NewSQLite(path)
	if e != nil {
		t.Fatal(e)
	}
	c.Close()
	d := &requeueRaceGateDriver{base: &sqlite.Driver{}, read: make(chan struct{}), resume: make(chan struct{})}
	driverName := fmt.Sprintf("requeue-schedule-race-gate-%d", time.Now().UnixNano())
	sql.Register(driverName, d)
	db, e := sql.Open(driverName, "file:"+path+"?_pragma=busy_timeout(5000)")
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	s := New(db)
	conf, e := s.CreateConfirmation("RACE", "entrada", "ant-1", time.Now(), 0)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`UPDATE pending_confirmations SET retry_count=10 WHERE id=?`, conf.ID); e != nil {
		t.Fatal(e)
	}
	scheduled := make(chan error, 1)
	go func() { scheduled <- s.ScheduleConfirmationRetry(conf.ID, 30*time.Second, time.Now()) }()
	select {
	case <-d.read:
	case <-time.After(3 * time.Second):
		t.Fatal("scheduler did not read retry_count")
	}
	n, e := s.RequeueStuckConfirmations(10)
	close(d.resume)
	if e != nil || n != 1 {
		t.Fatalf("requeue=%d %v", n, e)
	}
	if e = <-scheduled; e != nil {
		t.Fatal(e)
	}
	var retries int
	var next sql.NullString
	if e = db.QueryRow(`SELECT retry_count,CAST(next_attempt_at AS TEXT) FROM pending_confirmations WHERE id=?`, conf.ID).Scan(&retries, &next); e != nil {
		t.Fatal(e)
	}
	due, e := s.GetRetryableConfirmations(100, 10)
	if e != nil {
		t.Fatal(e)
	}
	t.Logf("requeued=%d retry_count=%d next_attempt_at=%v due=%d", n, retries, next, len(due))
	if retries != 0 || next.Valid || len(due) != 1 {
		t.Fatalf("requeue lost its immediate-due reset: retries=%d next=%v due=%d", retries, next, len(due))
	}
}
