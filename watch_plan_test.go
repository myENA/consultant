package consultant_test

import (
	"reflect"
	"testing"

	"github.com/hashicorp/consul/api"
	"github.com/myENA/consultant"
)

const (
	keyWatchKey           = "consultant/tests/keywatch"
	keyWatchOriginalValue = "original"
	keyWatchUpdatedValue  = "updated"
)

func TestKeyWatchPlan(t *testing.T) {
	var wp *consultant.WatchPlan

	client, server := makeClientAndServer(t, nil)
	defer server.Stop()

	t.Run("setup", func(t *testing.T) {
		kv := &api.KVPair{
			Key:   keyWatchKey,
			Value: []byte(keyWatchOriginalValue),
		}
		_, err := client.KV().Put(kv, nil)
		if nil != err {
			t.Logf("Unable to put key: %v", err)
			t.FailNow()
		}

		kv, _, err = client.KV().Get(keyWatchKey, nil)
		if nil != err {
			t.Logf("Unable to verify key put: %v", err)
			t.FailNow()
		}

		if nil == kv.Value {
			t.Logf("Expected kv value \"%s\", saw nil", keyWatchOriginalValue)
			t.FailNow()
		}

		if keyWatchOriginalValue != string(kv.Value) {
			t.Logf("Expected kv value \"%s\", saw \"%s\"", keyWatchOriginalValue, string(kv.Value))
			t.FailNow()
		}
	})

	t.Run("create watch plan", func(t *testing.T) {
		var err error
		wp, err = consultant.NewKeyPlan(client, keyWatchKey, api.QueryOptions{}, func(_ uint64, data interface{}) {
			kv, ok := data.(*api.KVPair)
			if !ok {
				t.Logf("Expected data to be \"*api.KVPair\", saw \"%s\"", reflect.TypeOf(data).Kind())
				t.FailNow()
			} else if nil == kv.Value {
				t.Logf("Expected kv value \"%s\", saw nil", keyWatchUpdatedValue)
				t.FailNow()
			} else if keyWatchUpdatedValue != string(kv.Value) {
				t.Logf("Expected kv value \"%s\", saw \"%s\"", keyWatchUpdatedValue, string(kv.Value))
				t.FailNow()
			} else {
				wp.Stop()
			}
		})

		if nil != err {
			t.Logf("Unable to create KeyWatchPlan: %v", err)
			t.FailNow()
		}

		wp.StopOnError = true
	})

	t.Run("run watch plan", func(t *testing.T) {
		errCh := make(chan error)
		go func() {
			errCh <- consultant.RunWatchPlan(wp)
		}()
		go func() {
			select {
			case err := <-errCh:
				if nil != err {
					t.Logf("Unable to run plan: %v", err)
					t.FailNow()
				}
			}
		}()
	})

	t.Run("update value", func(t *testing.T) {
		kv := &api.KVPair{
			Key:   keyWatchKey,
			Value: []byte(keyWatchUpdatedValue),
		}
		_, err := client.KV().Put(kv, nil)
		if nil != err {
			t.Logf("Unable to update kv: %v", err)
			t.FailNow()
		}

		kv, _, err = client.KV().Get(keyWatchKey, nil)
		if nil != err {
			t.Logf("Unable to verify key update: %v", err)
			t.FailNow()
		}

		if nil == kv.Value {
			t.Logf("Expected kv value \"%s\", saw nil", keyWatchUpdatedValue)
			t.FailNow()
		}

		if keyWatchUpdatedValue != string(kv.Value) {
			t.Logf("Expected kv value \"%s\", saw \"%s\"", keyWatchUpdatedValue, string(kv.Value))
			t.FailNow()
		}
	})
}
