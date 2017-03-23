package consultant_test

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/hashicorp/consul/api"
	"github.com/myENA/consultant"
)

const (
	keyWatchkey = "consultant/tests/keywatch"
)

func TestKeyWatchPlan(t *testing.T) {
	t.Parallel()

	var err error
	var kv *api.KVPair
	var wp *consultant.WatchPlan

	client, server := makeClient(t)
	defer server.Stop()

	kv = &api.KVPair{
		Key:   keyWatchkey,
		Value: []byte("original"),
	}

	t.Run("setup", func(t *testing.T) {
		_, err = client.KV().Put(kv, nil)
		if nil != err {
			t.Logf("Unable to put key: %v", err)
			t.FailNow()
		}
	})

	t.Run("create watch plan", func(t *testing.T) {
		wp, err = consultant.NewKeyPlan(client, keyWatchkey, api.QueryOptions{}, func(_ uint64, data interface{}) {
			kv, ok := data.(*api.KVPair)
			if !ok {
				t.Logf("Expected data to be \"*api.KVPair\", saw \"%s\"", reflect.TypeOf(data).Kind())
				t.FailNow()
			}

			fmt.Printf("%v\n", kv)
		})
		if nil != err {
			t.Logf("Unable to create KeyWatchPlan: %v", err)
			t.FailNow()
		}
	})

	t.Run("run watch plan", func(t *testing.T) {
		go func() {

		}()
	})

}
