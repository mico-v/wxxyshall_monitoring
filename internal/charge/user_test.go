package charge

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestGetOrderParsesThirdPartyObjectAndOmitsRaw(t *testing.T) {
	client := NewClient("https://example.test", "token")
	client.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"code":200,"order":{"orderid":"order-2","tranamt":10,"status":0,` +
			`"third_party":{"campus":"校区","building":"楼栋","room":"房间","roomid":"room-42"}},` +
			`"payList":[{"payid":65,"name":"卡账户","code":"CARDTSM","nopassword":0}]}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    r,
		}, nil
	})

	order, methods, err := client.GetOrderContext(context.Background(), "order-2")
	if err != nil {
		t.Fatal(err)
	}
	if order.Room == nil || order.Room.RoomID != "room-42" {
		t.Fatalf("third_party object not parsed: %+v", order)
	}
	if len(methods) != 1 || methods[0].Code != "CARDTSM" {
		t.Fatalf("unexpected payment methods: %+v", methods)
	}
	body, err := json.Marshal(order)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "third_party") || strings.Contains(string(body), "Raw") {
		t.Fatalf("sanitized order leaked raw fields: %s", body)
	}
}

func TestSubmitPasswordContextUsesIndexesOnceWithoutRetry(t *testing.T) {
	client := NewClient("https://example.test", "token")
	requests := 0
	var form string
	client.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		body, _ := io.ReadAll(r.Body)
		form = string(body)
		return &http.Response{
			StatusCode: http.StatusGatewayTimeout,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("timeout")),
			Request:    r,
		}, nil
	})

	_, err := client.SubmitPasswordContext(
		context.Background(),
		"order-3",
		PaymentMethod{PayID: 65, Code: "CARDTSM"},
		"452013",
		"uuid-3",
	)
	if err == nil {
		t.Fatal("gateway timeout should return an error")
	}
	if requests != 1 {
		t.Fatalf("payment requests = %d, want exactly one", requests)
	}
	if !strings.Contains(form, "password=452013") || !strings.Contains(form, "uuid=uuid-3") {
		t.Fatalf("unexpected payment form: %s", form)
	}
}

func TestGetPasswordMapRequiresSingleTenKeyMap(t *testing.T) {
	client := NewClient("https://example.test", "token")
	client.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"code":200,"data":{"passwordMap":{"uuid-4":["3","8","1","9","6","2","7","0","5","4"]}}}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    r,
		}, nil
	})
	passwordMap, err := client.GetPasswordMapContext(
		context.Background(),
		"order-4",
		PaymentMethod{PayID: 65, Code: "CARDTSM"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if passwordMap.UUID != "uuid-4" || len(passwordMap.Keys) != 10 || passwordMap.Keys[1] != 8 {
		t.Fatalf("unexpected password map: %+v", passwordMap)
	}
}
