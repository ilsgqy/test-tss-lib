package test

import (
	"encoding/json"
	"fmt"
	"github.com/bnb-chain/tss-lib/v2/common"
	"github.com/bnb-chain/tss-lib/v2/ecdsa/keygen"
	"github.com/bnb-chain/tss-lib/v2/test"
	"github.com/bnb-chain/tss-lib/v2/tss"
	"github.com/stretchr/testify/assert"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
)

const (
	testFixtureDirFormat  = "%s/ecdsa"
	testFixtureFileFormat = "keygen_data_%d.json"
)

func TestEcdsaKeygen(t *testing.T) {

	// 定义参与者数量和门限值
	n := 5 // 总参与者数量
	s := 2 // 门限值

	// --------------------- 阶段 1: 密钥生成 ---------------------
	fmt.Println("=== 密钥生成阶段 ===")

	pIDs := tss.GenerateTestPartyIDs(n) //	pIDs = tss.GenerateTestPartyIDs(testParticipants)

	p2pCtx := tss.NewPeerContext(pIDs)
	parties := make([]*keygen.LocalParty, 0, len(pIDs))

	errCh := make(chan *tss.Error, len(pIDs))
	outCh := make(chan tss.Message, len(pIDs))
	// 存储每个参与者的私钥分片
	endCh := make(chan *keygen.LocalPartySaveData, len(pIDs))

	updater := test.SharedPartyUpdater
	// 启动每个参与者的密钥生成流程
	for i := 0; i < len(pIDs); i++ {
		var P *keygen.LocalParty
		params := tss.NewParameters(tss.S256(), p2pCtx, pIDs[i], len(pIDs), s)
		// do not use in untrusted setting
		params.SetNoProofMod()
		// do not use in untrusted setting
		params.SetNoProofFac()
		P = keygen.NewLocalParty(params, outCh, endCh).(*keygen.LocalParty)
		fmt.Printf("Party %d is starting key generation\n", P.PartyID().Index)
		parties = append(parties, P)
		go func(P *keygen.LocalParty) {
			if err := P.Start(); err != nil {
				errCh <- err
			}
		}(P)
	}
	// PHASE: keygen
	var ended int32
keygen:
	for {
		fmt.Printf("ACTIVE GOROUTINES: %d\n", runtime.NumGoroutine())
		select {
		case err := <-errCh:
			common.Logger.Errorf("Error: %s", err)
			assert.FailNow(t, err.Error())
			break keygen

			// 广播消息给其他参与者
		case msg := <-outCh:
			dest := msg.GetTo()
			t.Logf("dest %s", dest)
			if dest == nil { // broadcast!
				for _, P := range parties {
					t.Logf(" P.PartyID()  %s partyID %d msgFrom %d ", P.PartyID(), P.PartyID().Index, msg.GetFrom().Index)
					if P.PartyID().Index == msg.GetFrom().Index {
						continue
					}
					go updater(P, msg, errCh)
				}
			} else { // point-to-point!
				if dest[0].Index == msg.GetFrom().Index {
					t.Fatalf("party %d tried to send a message to itself (%d)", dest[0].Index, msg.GetFrom().Index)
					return
				}
				go updater(parties[dest[0].Index], msg, errCh)
			}

		case result := <-endCh:
			// 输出当前处理完的参与者的相关信息
			// 协议完成，输出签名结果
			index, err := result.OriginalIndex()
			assert.NoErrorf(t, err, "should not be an error getting a party's index from save data")

			t.Log("-----------------------------------------")
			t.Log(index)
			t.Log("-----------------------------------------")

			tryWriteTestFixtureFile(t, index, *result)

			atomic.AddInt32(&ended, 1)
			if atomic.LoadInt32(&ended) == int32(len(pIDs)) {
				break keygen
			}

		}

	}
	fmt.Println("密钥生成完成！")

}

func tryWriteTestFixtureFile(t *testing.T, index int, data keygen.LocalPartySaveData) {
	fixtureFileName := makeTestFixtureFilePath(index)

	// fixture file does not already exist?
	// if it does, we won't re-create it here
	fi, err := os.Stat(fixtureFileName)
	if !(err == nil && fi != nil && !fi.IsDir()) {
		fd, err := os.OpenFile(fixtureFileName, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
		if err != nil {
			assert.NoErrorf(t, err, "unable to open fixture file %s for writing", fixtureFileName)
		}
		bz, err := json.Marshal(&data)
		if err != nil {
			t.Fatalf("unable to marshal save data for fixture file %s", fixtureFileName)
		}
		_, err = fd.Write(bz)
		if err != nil {
			t.Fatalf("unable to write to fixture file %s", fixtureFileName)
		}
		t.Logf("Saved a test fixture file for party %d: %s", index, fixtureFileName)
	} else {
		t.Logf("Fixture file already exists for party %d; not re-creating: %s", index, fixtureFileName)
	}
	//
}

func makeTestFixtureFilePath(partyIndex int) string {
	_, callerFileName, _, _ := runtime.Caller(0)
	srcDirName := filepath.Dir(callerFileName)
	fixtureDirName := fmt.Sprintf(testFixtureDirFormat, srcDirName)
	return fmt.Sprintf("%s/"+testFixtureFileFormat, fixtureDirName, partyIndex)
}
