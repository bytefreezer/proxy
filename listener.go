package bytefreezer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/bytedance/sonic"
	"github.com/bytedance/sonic/encoder"
	"github.com/gorilla/mux"
	flatten "github.com/jeremywohl/flatten"
	"github.com/n0needt0/go-goodies/log"
	"github.com/n0needt0/goodies/bytefreezer-proxy/internal/config"
	"github.com/n0needt0/goodies/bytefreezer-proxy/internal/services"
)

type UploadTask struct {
	DataFile   string
	SchemaFile string
	Timestamp  string
}

type SharedSchema struct {
	sync.Mutex
	Current map[string]string
}

var (
	udpTimestamp     string
	udpTimestampLock sync.Mutex
)

func SetUdpTimestamp() {
	udpTimestampLock.Lock()
	defer udpTimestampLock.Unlock()
	udpTimestamp = fmt.Sprintf("%d", time.Now().UnixNano())
}

func GetUdpTimestamp() string {
	udpTimestampLock.Lock()
	defer udpTimestampLock.Unlock()
	return udpTimestamp
}

func (s *SharedSchema) SaveParquetSchemaFile(timestamp string) (string, error) {
	s.Lock()
	defer s.Unlock()

	type fieldDef struct {
		Name string
		Type string
	}

	merged := make(map[string]string)

	// Load latest schema file if available
	matches, _ := filepath.Glob("/tmp/bytefreezer-*.schema.json")
	if len(matches) > 0 {
		sort.Strings(matches)
		latest := matches[len(matches)-1]
		content, err := os.ReadFile(latest)
		if err == nil {
			var prev struct {
				Fields []fieldDef `json:"Fields"`
			}
			if err := json.Unmarshal(content, &prev); err == nil {
				for _, f := range prev.Fields {
					merged[f.Name] = f.Type
				}
			}
		}
	}

	// Merge current schema into accumulated schema
	for k, v := range s.Current {
		merged[k] = v
	}

	// Build final sorted field list
	var schemaDef struct {
		Fields []fieldDef
	}
	for k, v := range merged {
		if k == "" || v == "" {
			log.Warnf("Skipping invalid schema field: key='%s', type='%s'", k, v)
			continue
		}
		schemaDef.Fields = append(schemaDef.Fields, fieldDef{Name: k, Type: v})
	}
	sort.Slice(schemaDef.Fields, func(i, j int) bool {
		return schemaDef.Fields[i].Name < schemaDef.Fields[j].Name
	})

	// Serialize schema
	var buf bytes.Buffer
	buf.WriteString(`{"Tag":"name=parquet-go-root","Fields":[`)
	for i, f := range schemaDef.Fields {
		var tag string
		switch f.Type {
		case "string":
			tag = fmt.Sprintf(`{"Tag":"name=%s, type=BYTE_ARRAY, convertedtype=UTF8, encoding=PLAIN, repetitiontype=OPTIONAL"}`, f.Name)
		case "int", "int32", "int64":
			tag = fmt.Sprintf(`{"Tag":"name=%s, type=INT64, repetitiontype=OPTIONAL"}`, f.Name)
		case "float32", "float64":
			tag = fmt.Sprintf(`{"Tag":"name=%s, type=DOUBLE, repetitiontype=OPTIONAL"}`, f.Name)
		case "bool", "boolean":
			tag = fmt.Sprintf(`{"Tag":"name=%s, type=BOOLEAN, repetitiontype=OPTIONAL"}`, f.Name)
		default:
			log.Warnf("Unknown schema type: %s for key %s. Defaulting to string", f.Type, f.Name)
			tag = fmt.Sprintf(`{"Tag":"name=%s, type=BYTE_ARRAY, convertedtype=UTF8, encoding=PLAIN, repetitiontype=OPTIONAL"}`, f.Name)
		}
		buf.WriteString(tag)
		if i < len(schemaDef.Fields)-1 {
			buf.WriteString(",")
		}
	}
	buf.WriteString(`]}`)

	// Write schema to file
	path := fmt.Sprintf("/tmp/bytefreezer-%s.schema.json", timestamp)
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		return "", err
	}

	return path, nil
}

func inferType(value interface{}) string {
	switch v := value.(type) {
	case bool:
		return "boolean"
	case float64:
		if float64(int64(v)) == v {
			return "int64"
		}
		return "float64"
	case string:
		return "string"
	default:
		return "string"
	}
}

func PrepareEnvelope(payload []byte) (map[string]interface{}, map[string]struct{}, error) {
	var original map[string]interface{}
	if err := sonic.Unmarshal(payload, &original); err != nil {
		return nil, nil, err
	}

	flat, err := flatten.Flatten(original, "", flatten.DotStyle)
	if err != nil {
		return nil, nil, err
	}

	envelope := map[string]interface{}{
		"Timestamp": time.Now().UTC().Format(time.RFC3339Nano),
	}

	for k, v := range flat {
		envelope[k] = v
	}

	schemaKeys := make(map[string]struct{}, len(envelope))
	for k := range envelope {
		schemaKeys[k] = struct{}{}
	}

	return envelope, schemaKeys, nil
}

func SaveAndNotify(schema *SharedSchema, dataPath string, notify chan UploadTask, timestamp string) {
	schemaPath, err := schema.SaveParquetSchemaFile(timestamp)
	if err != nil {
		log.Errorf("Failed to save schema: %v", err)
		return
	}
	notify <- UploadTask{DataFile: dataPath, SchemaFile: schemaPath, Timestamp: timestamp}
}

type UdpListener struct {
	Services     *services.Services
	addr         *net.UDPAddr
	Config       *config.Config
	quit         chan bool
	uploadNotify chan UploadTask
	bufferPool   sync.Pool
	Schema       *SharedSchema
}

type WebhookListener struct {
	Services     *services.Services
	Config       *config.Config
	uploadNotify chan UploadTask
	Schema       *SharedSchema
	quit         chan bool
	wg           sync.WaitGroup
	httpServer   *http.Server
}

func NewUdpListener(services *services.Services, config *config.Config, uploadNotify chan UploadTask, schema *SharedSchema) *UdpListener {
	return &UdpListener{
		Services:     services,
		addr:         &net.UDPAddr{IP: net.ParseIP(config.Bytefreezer.Host), Port: config.Bytefreezer.Port},
		Config:       config,
		uploadNotify: uploadNotify,
		quit:         make(chan bool),
		bufferPool: sync.Pool{
			New: func() interface{} {
				return make([]byte, config.Bytefreezer.ReadBufferSizeBytes)
			},
		},
		Schema: schema,
	}
}

func NewWebhookListener(services *services.Services, config *config.Config, uploadNotify chan UploadTask, schema *SharedSchema) *WebhookListener {
	return &WebhookListener{
		Services:     services,
		Config:       config,
		uploadNotify: uploadNotify,
		Schema:       schema,
		quit:         make(chan bool),
	}
}

func (l *UdpListener) Listen() error {
	ln, err := net.ListenUDP("udp", l.addr)
	if err != nil {
		log.Fatal(err.Error())
	}
	defer ln.Close()

	err = ln.SetReadBuffer(l.Config.Bytefreezer.ReadBufferSizeBytes)
	if err != nil {
		log.Fatal(err.Error())
	}

	log.Printf("udp server up and listening on %s:%d", l.Config.Bytefreezer.Host, l.Config.Bytefreezer.Port)

	go l.handleUDPConnection(ln)

	<-l.quit
	log.Info("UDP Server connection closing")
	return nil
}

func (l *UdpListener) handleUDPConnection(conn *net.UDPConn) {
	var (
		file      *os.File
		filePath  string
		lineCount int
		totalSize int64
		timestamp string
	)

	resetBatch := func() {
		if file != nil {
			file.Close()
			SaveAndNotify(l.Schema, filePath, l.uploadNotify, timestamp)
		}
		file = nil
		filePath = ""
		lineCount = 0
		totalSize = 0
		timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}

	newFile := func() (string, error) {
		timestamp = fmt.Sprintf("%d", time.Now().UnixNano())

		fileName := fmt.Sprintf("/tmp/bytefreezer-%s.udp.ndjson", timestamp)
		f, err := os.Create(fileName)
		if err != nil {
			return "", err
		}
		file = f
		filePath = fileName
		return timestamp, err
	}

	for {
		conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		buf := l.AllocateBuffer()
		readLen, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			l.DeallocateBuffer(buf)
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			log.Errorf("UDP read error: %s", err.Error())
			continue
		}

		payload := bytes.TrimSpace(buf[:readLen])
		payload = bytes.Trim(payload, "\x08\x00")
		l.DeallocateBuffer(buf)

		envelope, flatKeys, err := PrepareEnvelope(payload)
		if err != nil {
			log.Errorf("UDP payload error: %v", err)
			continue
		}

		l.Schema.Lock()
		for k := range flatKeys {
			if envelope[k] == nil {
				continue
			}
			if k == "" {
				log.Warn("Skipping empty key in envelope")
				continue
			}

			if oldType, ok := l.Schema.Current[k]; ok {
				newType := inferType(envelope[k])
				if oldType != newType {
					l.Schema.Current[k] = "string"
				}
			} else {
				l.Schema.Current[k] = inferType(envelope[k])
			}
		}
		l.Schema.Unlock()

		line, err := encoder.Encode(&envelope, encoder.SortMapKeys)
		if err != nil {
			log.Errorf("Encoding UDP payload failed: %v", err)
			continue
		}

		if file == nil {
			if _, err := newFile(); err != nil {
				log.Errorf("Failed to create UDP temp file: %v", err)
				continue
			}
		}

		if _, err := file.Write(append([]byte(line), '\n')); err != nil {
			log.Errorf("Failed to write to UDP temp file: %v", err)
			resetBatch()
			continue
		}

		lineCount++
		totalSize += int64(len(line))

		if (l.Config.Bytefreezer.MaxBatchRows > 0 && lineCount >= l.Config.Bytefreezer.MaxBatchRows) ||
			(l.Config.Bytefreezer.MaxBatchBytes > 0 && totalSize >= l.Config.Bytefreezer.MaxBatchBytes) {
			resetBatch()
		}
	}
}

func (l *UdpListener) AllocateBuffer() []byte {
	return l.bufferPool.Get().([]byte)
}

func (l *UdpListener) DeallocateBuffer(buf []byte) {
	l.bufferPool.Put(buf)
}

func (l *UdpListener) Shutdown() error {
	log.Info("UDP Server shutting down")
	close(l.quit)
	return nil
}

func (l *WebhookListener) Listen() error {
	r := mux.NewRouter()

	r.HandleFunc("/api/v2/{token}", func(w http.ResponseWriter, r *http.Request) {
		l.handlerWebhookConnection(r.Context(), w, r)
	}).Methods("POST")

	r.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}).Methods("GET")

	l.httpServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", l.Config.Bytefreezer.WebhookPort),
		Handler: r,
	}

	l.wg.Add(1)
	go func() {
		defer l.wg.Done()
		log.Infof("Starting webhook server on :%d", l.Config.Bytefreezer.WebhookPort)
		if err := l.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("Error starting webhook server: " + err.Error())
		}
	}()

	return nil
}

func (l *WebhookListener) Shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if l.httpServer != nil {
		if err := l.httpServer.Shutdown(ctx); err != nil {
			log.Errorf("Webhook server shutdown error: %v", err)
		} else {
			log.Info("Webhook server shutdown complete")
		}
	}
	l.wg.Wait()
}

func (l *WebhookListener) handlerWebhookConnection(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	token := vars["token"]

	if token != l.Config.Bytefreezer.Token {
		log.Errorf("Invalid token: %s", token)
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("Unauthorized"))
		return
	}

	payload, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Invalid body"))
		return
	}
	defer r.Body.Close()

	envelope, flatKeys, err := PrepareEnvelope(payload)
	if err != nil {
		log.Errorf("Webhook payload error: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	l.Schema.Lock()
	for k := range flatKeys {
		if envelope[k] == nil {
			continue
		}
		if k == "" {
			log.Warn("Skipping empty key in envelope")
			continue
		}
		if oldType, ok := l.Schema.Current[k]; ok {
			newType := inferType(envelope[k])
			if oldType != newType {
				l.Schema.Current[k] = "string"
			}
		} else {
			l.Schema.Current[k] = inferType(envelope[k])
		}
	}
	l.Schema.Unlock()

	line, err := encoder.Encode(&envelope, encoder.SortMapKeys)
	if err != nil {
		log.Errorf("Encoding webhook payload failed: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Set global timestamp before generating filenames
	ts := fmt.Sprintf("%d", time.Now().UnixNano())
	fileName := fmt.Sprintf("/tmp/bytefreezer-%s.tcp.ndjson", ts)
	tmpfile, err := os.Create(fileName)
	if err != nil {
		log.Errorf("Failed to create webhook temp file: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer tmpfile.Close()

	if _, err := tmpfile.Write(append([]byte(line), '\n')); err != nil {
		log.Errorf("Failed to write webhook temp file: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	SaveAndNotify(l.Schema, tmpfile.Name(), l.uploadNotify, ts)
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Accepted"))
}
