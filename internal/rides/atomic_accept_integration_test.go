package rides

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This test deliberately uses the real PostgreSQL concurrency semantics. Set
// TEST_DATABASE_URL to a migrated disposable database in CI.
func TestAtomicAcceptRide_ExactlyOneConcurrentWinner(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewRepository(db)
	riderID, rideID := uuid.New(), uuid.New()
	drivers := make([]uuid.UUID, 20)
	_, err = db.Exec(ctx, `INSERT INTO users(id,email,phone_number,password_hash,first_name,last_name,role) VALUES($1,$2,$3,'x','Test','Rider','rider')`, riderID, riderID.String()+"@test.invalid", "+1"+riderID.String()[:15])
	if err != nil {
		t.Fatal(err)
	}
	for i := range drivers {
		drivers[i] = uuid.New()
		_, err = db.Exec(ctx, `INSERT INTO users(id,email,phone_number,password_hash,first_name,last_name,role) VALUES($1,$2,$3,'x','Test','Driver','driver')`, drivers[i], drivers[i].String()+"@test.invalid", "+1"+drivers[i].String()[:15])
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err = db.Exec(ctx, `INSERT INTO rides(id,rider_id,status,pickup_latitude,pickup_longitude,pickup_address,dropoff_latitude,dropoff_longitude,dropoff_address,estimated_distance,estimated_duration,estimated_fare) VALUES($1,$2,'requested',1,1,'a',2,2,'b',1,1,10)`, rideID, riderID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), `DELETE FROM rides WHERE id=$1`, rideID)
		_, _ = db.Exec(context.Background(), `DELETE FROM users WHERE id=$1 OR id=ANY($2)`, riderID, drivers)
	})
	var winners int32
	var wg sync.WaitGroup
	for _, driverID := range drivers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := repo.AtomicAcceptRide(ctx, rideID, driverID)
			if err != nil {
				t.Errorf("accept: %v", err)
				return
			}
			if ok {
				atomic.AddInt32(&winners, 1)
			}
		}()
	}
	wg.Wait()
	if winners != 1 {
		t.Fatalf("expected exactly one winner, got %d", winners)
	}
}
