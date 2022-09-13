package wechat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	log "maunium.net/go/maulogger/v2"
)

var (
	errMissingToken = Error{
		HTTPStatus: http.StatusForbidden,
		ErrorCode:  "M_MISSING_TOKEN",
		Message:    "Missing authorization header",
	}
	errUnknownToken = Error{
		HTTPStatus: http.StatusForbidden,
		ErrorCode:  "M_UNKNOWN_TOKEN",
		Message:    "Unknown authorization token",
	}

	ErrWebsocketNotConnected = errors.New("websocket not connected")
	ErrWebsocketClosed       = errors.New("websocket closed before response received")
)

type WechatService struct {
	log log.Logger

	addr   string
	secret string

	server    *http.Server
	conn      *websocket.Conn
	connLock  sync.Mutex
	writeLock sync.Mutex

	clients     map[string]*WechatClient
	clientsLock sync.RWMutex

	websocketRequests     map[int]chan<- *WebsocketCommand
	websocketRequestsLock sync.RWMutex
	wsRequestID           int32
}

var upgrader = websocket.Upgrader{}

func (ws *WechatService) Conn() *websocket.Conn {
	return ws.conn
}

func (ws *WechatService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Basic ") {
		errMissingToken.Write(w)
		return
	}

	if authHeader[len("Basic "):] != ws.secret {
		errUnknownToken.Write(w)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		ws.log.Warnln("Failed to upgrade websocket request:", err)
		return
	}

	ws.log.Infoln("WechatService websocket connected")
	defer func() {
		ws.log.Infoln("Disconnected from websocket")
		ws.connLock.Lock()
		if ws.conn == conn {
			ws.conn = nil
		}
		ws.connLock.Unlock()
		_ = conn.Close()
	}()

	ws.connLock.Lock()
	ws.conn = conn
	ws.connLock.Unlock()

	for {
		var msg WebsocketMessage
		err := conn.ReadJSON(&msg)
		if err != nil {
			ws.log.Warnln("Error reading from websocket:", err)
			break
		}

		if msg.Command == "" {
			ws.clientsLock.RLock()
			client, ok := ws.clients[msg.MXID]
			if !ok {
				ws.log.Warnln("Dropping event to %d: no receiver", msg.MXID)
				continue
			}
			go client.HandleEvent(&msg)
			ws.clientsLock.RUnlock()
		} else if msg.Command == CommandPing {
			// TODO:
		} else if msg.Command == CommandResponse || msg.Command == CommandError {
			ws.websocketRequestsLock.RLock()
			respChan, ok := ws.websocketRequests[msg.ReqID]
			if ok {
				select {
				case respChan <- &msg.WebsocketCommand:
				default:
					ws.log.Warnln("Failed to handle response to %d: channel didn't accept response", msg.ReqID)
				}
			} else {
				ws.log.Warnln("Dropping response to %d: unknown request ID", msg.ReqID)
			}
			ws.websocketRequestsLock.RUnlock()
		}
	}
}

func (ws *WechatService) Start() {
	ws.log.Infoln("WechatService starting to listen on", ws.addr)
	err := ws.server.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		ws.log.Fatalln("Error in listener:", err)
	}
}

func (ws *WechatService) Stop() {
	go func(oldConn *websocket.Conn) {
		if oldConn == nil {
			return
		}
		msg := websocket.FormatCloseMessage(
			websocket.CloseGoingAway,
			fmt.Sprintf(`{"command": "%s", "status": "server_shutting_down"}`, CommandDisconnect),
		)
		_ = oldConn.WriteControl(websocket.CloseMessage, msg, time.Now().Add(3*time.Second))
		_ = oldConn.Close()
	}(ws.conn)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := ws.server.Shutdown(ctx)
	if err != nil {
		ws.log.Warnln("Failed to close server:", err)
	}
}

func (ws *WechatService) CreateClient(mxid string, handler func(*WebsocketMessage)) *WechatClient {
	ws.clientsLock.Lock()
	defer ws.clientsLock.Unlock()

	client := NewWechatClient(mxid, ws, handler)
	ws.clients[mxid] = client

	return client
}

func (ws *WechatService) RemoveClient(mxid string) {
	ws.clientsLock.Lock()
	defer ws.clientsLock.Unlock()

	delete(ws.clients, mxid)
}

func (ws *WechatService) RequestWebsocket(ctx context.Context, cmd *WebsocketRequest, response interface{}) error {
	cmd.ReqID = int(atomic.AddInt32(&ws.wsRequestID, 1))
	respChan := make(chan *WebsocketCommand, 1)

	ws.addWebsocketResponseWaiter(cmd.ReqID, respChan)
	defer ws.removeWebsocketResponseWaiter(cmd.ReqID, respChan)

	err := ws.SendWebsocket(cmd)
	if err != nil {
		return err
	}

	select {
	case resp := <-respChan:
		if resp.Command == CommandClosed {
			return ErrWebsocketClosed
		} else if resp.Command == CommandError {
			var respErr ErrorResponse
			err = json.Unmarshal(resp.Data, &respErr)
			if err != nil {
				return fmt.Errorf("failed to parse error JSON: %w", err)
			}
			return &respErr
		} else if response != nil {
			err = json.Unmarshal(resp.Data, &response)
			if err != nil {
				ws.log.Warnln(string(resp.Data))
				return fmt.Errorf("failed to parse response JSON: %w", err)
			}
			return nil
		} else {
			return nil
		}
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (ws *WechatService) SendWebsocket(cmd *WebsocketRequest) error {
	conn := ws.conn
	if cmd == nil {
		return nil
	} else if conn == nil {
		return ErrWebsocketNotConnected
	}
	ws.writeLock.Lock()
	defer ws.writeLock.Unlock()
	if cmd.Deadline == 0 {
		cmd.Deadline = 3 * time.Minute
	}
	_ = conn.SetWriteDeadline(time.Now().Add(cmd.Deadline))
	return conn.WriteJSON(cmd)
}

func (ws *WechatService) addWebsocketResponseWaiter(reqID int, waiter chan<- *WebsocketCommand) {
	ws.websocketRequestsLock.Lock()
	ws.websocketRequests[reqID] = waiter
	ws.websocketRequestsLock.Unlock()
}

func (ws *WechatService) removeWebsocketResponseWaiter(reqID int, waiter chan<- *WebsocketCommand) {
	ws.websocketRequestsLock.Lock()
	existingWaiter, ok := ws.websocketRequests[reqID]
	if ok && existingWaiter == waiter {
		delete(ws.websocketRequests, reqID)
	}
	close(waiter)
	ws.websocketRequestsLock.Unlock()
}

func NewWechatService(addr, secret string, log log.Logger) *WechatService {
	service := &WechatService{
		log:               log,
		addr:              addr,
		secret:            secret,
		clients:           make(map[string]*WechatClient),
		websocketRequests: make(map[int]chan<- *WebsocketCommand),
	}
	service.server = &http.Server{
		Addr:    service.addr,
		Handler: service,
	}

	return service
}

// Error represents a Matrix protocol error.
type Error struct {
	HTTPStatus int       `json:"-"`
	ErrorCode  ErrorCode `json:"errcode"`
	Message    string    `json:"error"`
}

func (err Error) Write(w http.ResponseWriter) {
	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(err.HTTPStatus)
	_ = Respond(w, &err)
}

// ErrorCode is the machine-readable code in an Error.
type ErrorCode string

// Native ErrorCodes
const (
	ErrUnknownToken ErrorCode = "M_UNKNOWN_TOKEN"
	ErrBadJSON      ErrorCode = "M_BAD_JSON"
	ErrNotJSON      ErrorCode = "M_NOT_JSON"
	ErrUnknown      ErrorCode = "M_UNKNOWN"
)

type ErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (er *ErrorResponse) Error() string {
	return fmt.Sprintf("%s: %s", er.Code, er.Message)
}

// Respond responds to a HTTP request with a JSON object.
func Respond(w http.ResponseWriter, data interface{}) error {
	w.Header().Add("Content-Type", "application/json")
	dataStr, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = w.Write(dataStr)
	return err
}
