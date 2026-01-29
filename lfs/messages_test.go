package lfs

import (
	"bytes"
	"testing"

	"github.com/bytedance/sonic"
	"github.com/bytedance/sonic/encoder"
)

func TestWriteMessages(t *testing.T) {
	buf := &bytes.Buffer{}
	enc := encoder.NewStreamEncoder(buf)

	WriteInitErrorMessage(enc, 400, "bad")
	var initErr InitErrorMessage
	if err := sonic.ConfigFastest.Unmarshal(buf.Bytes(), &initErr); err != nil {
		t.Fatalf("unmarshal init error: %v", err)
	}
	if initErr.Error.Code != 400 {
		t.Fatalf("unexpected code: %d", initErr.Error.Code)
	}

	buf.Reset()
	WriteErrorMessage(enc, "oid", 500, "fail")
	var errMsg ErrorMessage
	if err := sonic.ConfigFastest.Unmarshal(buf.Bytes(), &errMsg); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if errMsg.Oid != "oid" || errMsg.Error.Code != 500 {
		t.Fatalf("unexpected error message: %+v", errMsg)
	}

	buf.Reset()
	WriteCompleteMessage(enc, "oid", "path")
	var complete CompleteMessage
	if err := sonic.ConfigFastest.Unmarshal(buf.Bytes(), &complete); err != nil {
		t.Fatalf("unmarshal complete: %v", err)
	}
	if complete.Path != "path" || complete.Oid != "oid" {
		t.Fatalf("unexpected complete message: %+v", complete)
	}

	buf.Reset()
	WriteProgressMessage(enc, "oid", 10, 5)
	var progress ProgressResponse
	if err := sonic.ConfigFastest.Unmarshal(buf.Bytes(), &progress); err != nil {
		t.Fatalf("unmarshal progress: %v", err)
	}
	if progress.Oid != "oid" || progress.BytesSoFar != 10 || progress.BytesSinceLast != 5 {
		t.Fatalf("unexpected progress message: %+v", progress)
	}

}
