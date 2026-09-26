package codec_test

import (
	"bytes"
	"testing"

	ginjson "github.com/gin-gonic/gin/codec/json"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"bgin/internal/platform/codec"
)

type samplePayload struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func TestGoJSONAdapter(t *testing.T) {
	codec.EnableGoJSON()

	original := samplePayload{Name: "bgin", Count: 42}
	data, err := ginjson.API.Marshal(original)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"name":"bgin"`)

	var decoded samplePayload
	err = ginjson.API.Unmarshal(data, &decoded)
	require.NoError(t, err)
	assert.Equal(t, original, decoded)

	// 测试流式编码器与解码器
	var buf bytes.Buffer
	enc := ginjson.API.NewEncoder(&buf)
	err = enc.Encode(original)
	require.NoError(t, err)

	dec := ginjson.API.NewDecoder(&buf)
	var streamDecoded samplePayload
	err = dec.Decode(&streamDecoded)
	require.NoError(t, err)
	assert.Equal(t, original, streamDecoded)
}
