package checkpointing

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
)

func TaggedHash(tag string, datas ...[]byte) []byte {
	tagSum := sha256.Sum256([]byte(tag))

	h := sha256.New()
	h.Write(tagSum[:])
	h.Write(tagSum[:])
	for _, data := range datas {
		h.Write(data)
	}
	return h.Sum(nil)
}

func Sha256(data []byte) []byte {
	h := sha256.New()
	h.Write(data[:])
	return h.Sum(nil)
}

func TaprootSignatureHash(tx []byte, utxo []byte, hash_type byte) ([]byte, error) {
	if hash_type != 0x00 {
		return nil, errors.New("only support SIGHASH_DEFAULT (0x00)")
	}

	var ss []byte

	ext_flag := 0x00
	ss = append(ss, byte(ext_flag))

	// Epoch
	ss = append(ss, 0x00)

	// version (4 bytes)
	ss = append(ss, tx[:4]...)
	// locktime
	ss = append(ss, tx[len(tx)-4:]...)
	// Transaction level data
	// !IMPORTANT! This only work because we have 1 utxo.
	// Please check https://github.com/bitcoin/bips/blob/master/bip-0341.mediawiki#common-signature-message

	// Previous output (txid + index = 36 bytes)
	ss = append(ss, Sha256(tx[5:5+36])...)

	// Amount in the previous output (8 bytes)
	ss = append(ss, Sha256(utxo[0:8])...)

	// PubScript in the previous output (35 bytes)
	ss = append(ss, Sha256(utxo[8:8+35])...)

	// Sequence (4 bytes)
	ss = append(ss, Sha256(tx[5+36+1:5+36+1+4])...)

	// Adding new txouts
	ss = append(ss, Sha256(tx[47:len(tx)-4])...)

	// spend type (here key path spending)
	ss = append(ss, 0x00)

	// Input index
	ss = append(ss, []byte{0, 0, 0, 0}...)

	return TaggedHash("TapSighash", ss), nil
}

func jsonRPC(payload string) map[string]interface{} {
	// ZONDAX TODO
	// This needs to be in a config file
	url := "http://127.0.0.1:18443"
	method := "POST"

	user := "satoshi"
	password := "amiens"

	client := &http.Client{}

	p := strings.NewReader(payload)
	req, err := http.NewRequest(method, url, p)

	if err != nil {
		fmt.Println(err)
		return nil
	}
	req.SetBasicAuth(user, password)

	res, err := client.Do(req)
	if err != nil {
		fmt.Println(err)
		return nil
	}
	defer res.Body.Close()

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		fmt.Println(err)
		return nil
	}

	var result map[string]interface{}
	json.Unmarshal([]byte(body), &result)
	return result
}
