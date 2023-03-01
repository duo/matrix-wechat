package wechat

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	log "maunium.net/go/maulogger/v2"
)

var (
	errMissingToken = ErrorResponse{
		HTTPStatus: http.StatusForbidden,
		Code:       "M_MISSING_TOKEN",
		Message:    "Missing authorization header",
	}
	errUnknownToken = ErrorResponse{
		HTTPStatus: http.StatusForbidden,
		Code:       "M_UNKNOWN_TOKEN",
		Message:    "Unknown authorization token",
	}

	ErrWebsocketNotConnected = errors.New("websocket not connected")
	ErrWebsocketClosed       = errors.New("websocket closed before response received")

	requestTimeout = 30 * time.Second

	upgrader = websocket.Upgrader{}
)

type Conn struct {
	conn      *websocket.Conn
	writeLock sync.Mutex
}

func (c *Conn) sendMessage(msg *Message) error {
	c.writeLock.Lock()
	defer c.writeLock.Unlock()

	return c.conn.WriteJSON(msg)
}

func (c *Conn) close() {
	c.writeLock.Lock()
	defer c.writeLock.Unlock()

	msg := websocket.FormatCloseMessage(websocket.CloseGoingAway, "")
	_ = c.conn.WriteControl(websocket.CloseMessage, msg, time.Now().Add(3*time.Second))
	_ = c.conn.Close()
}

type WechatService struct {
	log log.Logger

	addr   string
	secret string

	server *http.Server

	clients     map[string]*WechatClient
	clientsLock sync.RWMutex

	conns    map[string]*Conn
	connLock sync.RWMutex

	requests     map[int64]chan<- *Response
	requestsLock sync.RWMutex
	requestID    int64
}

func NewWechatService(addr, secret string, log log.Logger) *WechatService {
	service := &WechatService{
		log:      log.Sub("WeChat"),
		addr:     addr,
		secret:   secret,
		clients:  make(map[string]*WechatClient),
		conns:    make(map[string]*Conn),
		requests: make(map[int64]chan<- *Response),
	}
	service.server = &http.Server{
		Addr:    service.addr,
		Handler: service,
	}

	return service
}

func (ws *WechatService) NewClient(mxid string) *WechatClient {
	ws.clientsLock.Lock()
	defer ws.clientsLock.Unlock()

	client, ok := ws.clients[mxid]
	if !ok {
		client = newWechatClient(mxid, ws.request, ws.log)
		ws.clients[mxid] = client
	}

	return client
}

func (ws *WechatService) Start() {
	ws.log.Infoln("WechatService starting to listen on", ws.addr)

	err := ws.server.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		ws.log.Fatalln("Error in listener:", err)
	}
}

func (ws *WechatService) Stop() {
	ws.log.Infofln("WechatService stopping")

	ws.connLock.Lock()
	defer ws.connLock.Unlock()
	for _, conn := range ws.conns {
		conn.close()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := ws.server.Shutdown(ctx)
	if err != nil {
		ws.log.Warnln("Failed to close server:", err)
	}
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

	key := conn.RemoteAddr().String()

	ws.log.Infoln("Agent connected from:", key)
	defer func() {
		ws.log.Infoln("Agent disconnected from:", key)
		ws.connLock.Lock()
		delete(ws.conns, key)
		ws.connLock.Unlock()
		_ = conn.Close()
	}()

	ws.connLock.Lock()
	ws.conns[key] = &Conn{conn: conn}
	ws.connLock.Unlock()

	for {
		var msg Message
		err := conn.ReadJSON(&msg)
		if err != nil {
			ws.log.Warnln("Error reading from websocket:", err)
			break
		}

		switch msg.Type {
		case MsgRequest:
			request := msg.Data.(*Request)
			if request.Type == ReqEvent {
				ws.clientsLock.RLock()
				client, ok := ws.clients[msg.MXID]
				ws.clientsLock.RUnlock()
				if ok {
					go client.processFunc(request.Data.(*Event))
				} else {
					ws.log.Warnln("Dropping event for %d: no receiver", msg.MXID)
				}
			} else {
				ws.log.Warnfln("Request %s not support", request.Type)
			}
		case MsgResponse:
			ws.requestsLock.RLock()
			respChan, ok := ws.requests[msg.ID]
			ws.requestsLock.RUnlock()
			if ok {
				select {
				case respChan <- msg.Data.(*Response):
				default:
					ws.log.Warnfln("Failed to handle response to %d: channel didn't accept response", msg.ID)
				}
			} else {
				ws.log.Warnfln("Dropping response to %d: unknown request ID", msg.ID)
			}
		}
	}
}

func (ws *WechatService) request(client *WechatClient, req *Request) (any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	msg := &Message{
		ID:   atomic.AddInt64(&ws.requestID, 1),
		MXID: client.mxid,
		Type: MsgRequest,
		Data: req,
	}
	respChan := make(chan *Response, 1)

	ws.addResponseWaiter(msg.ID, respChan)
	defer ws.removeResponseWaiter(msg.ID, respChan)

	conn := ws.getConn(client)
	if conn == nil {
		return nil, errors.New("no agent connection avaiable")
	}

	ws.log.Debugfln("Send request message #%d %s", msg.ID, req.Type)
	if err := conn.sendMessage(msg); err != nil {
		return nil, err
	}

	select {
	case resp := <-respChan:
		ws.log.Debugfln("Receive response message #%d %s", msg.ID, resp.Type)
		//return resp.Data, resp.Error
		if resp.Error != nil {
			return nil, resp.Error
		} else {
			return resp.Data, nil
		}
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (ws *WechatService) addResponseWaiter(reqID int64, waiter chan<- *Response) {
	ws.requestsLock.Lock()
	ws.requests[reqID] = waiter
	ws.requestsLock.Unlock()
}

func (ws *WechatService) removeResponseWaiter(reqID int64, waiter chan<- *Response) {
	ws.requestsLock.Lock()
	existingWaiter, ok := ws.requests[reqID]
	if ok && existingWaiter == waiter {
		delete(ws.requests, reqID)
	}
	ws.requestsLock.Unlock()
	close(waiter)
}

func (ws *WechatService) getConn(client *WechatClient) *Conn {
	ws.connLock.RLock()
	defer ws.connLock.RUnlock()

	if conn, ok := ws.conns[client.getConnKey()]; ok {
		return conn
	} else {
		for k, v := range ws.conns {
			client.setConnKey(k)
			return v
		}
	}

	return nil
}
