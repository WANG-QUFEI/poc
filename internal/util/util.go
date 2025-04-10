package util

import (
	"encoding/json"

	"github.com/rs/zerolog/log"
)

func JSONMarshalIgnoreErr(v any) []byte {
	if v == nil {
		return []byte("null")
	}

	bs, err := json.Marshal(v)
	if err != nil {
		log.Err(err).Msg("json marshal error")
		return []byte("{}")
	}

	return bs
}

func FormatPath(path string) string {
	if path == "" {
		return "/"
	}
	if path[0] != '/' {
		path = "/" + path
	}
	if path[len(path)-1] != '/' {
		path += "/"
	}
	return path
}
