# Test Coverage Refactoring Plan

## Mevcut Durum

**Coverage Oranları:**
- internal/api: **87.1%** (Hedefe en uzak modül)
- internal/config: 86.7%
- internal/jmap: 86.7%
- internal/vacation: 87.1%
- internal/carddav: 87.3%
- internal/webhook: 87.8%
- internal/auth: 88.0%
- internal/push: 88.7%
- internal/circuitbreaker: 89.9%
- internal/storage: 90.5%
- internal/websocket: 91.7%
- internal/cli: 92.7%
- internal/store: 93.3%
- internal/mcp: 94.2%
- internal/tls: 95.9%
- internal/search: 97.9%
- internal/av: 100.0% ✅
- internal/metrics: 100.0% ✅
- internal/tracing: 100.0% ✅

**Toplam:** 19 modül, 3'ü 100%, 16'sı eksik

---

## Hedef

**Tüm modüller için minimum 90% coverage**
- Priority 1: internal/api (87.1% → 90%+)
- Priority 2: internal/config (86.7% → 90%+)
- Priority 3: internal/jmap (86.7% → 90%+)
- Priority 4: Diğer 85-89% arası modüller

---

## Faz 1: Interface Tabanlı Mock Sistemi (API Modülü)

### 1.1 Depedency Interface Tanımları

```go
// internal/api/interfaces.go

// QueueManager interface for queue operations
type QueueManager interface {
    GetStats() (*queue.Stats, error)
    GetEntries(status string) ([]*queue.Entry, error)
    RetryEntry(id string) error
    DeleteEntry(id string) error
}

// MessageStore interface for storage operations
type MessageStore interface {
    GetMessage(mailbox, uid string) (*storage.Message, error)
    ListMessages(mailbox string) ([]*storage.Message, error)
    StoreMessage(mailbox string, msg *storage.Message) error
    DeleteMessage(mailbox, uid string) error
}

// PushService interface for push notifications
type PushService interface {
    Subscribe(userID string, sub *push.Subscription) error
    Unsubscribe(userID, subscriptionID string) error
    SendNotification(userID string, notif *push.Notification) error
    GetVAPIDPublicKey() string
}

// VacationManager interface for vacation auto-reply
type VacationManager interface {
    GetConfig(userID string) (*vacation.Config, error)
    SetConfig(userID string, cfg *vacation.Config) error
    DeleteConfig(userID string) error
    ListActive() ([]string, error)
}

// FilterManager interface for email filters
type FilterManager interface {
    GetUserFilters(userID string) ([]*EmailFilter, error)
    GetFilter(userID, filterID string) (*EmailFilter, error)
    SaveFilter(filter *EmailFilter) error
    DeleteFilter(userID, filterID string) error
    ReorderFilters(userID string, filterIDs []string) error
}
```

### 1.2 Mock Implementasyonları

```go
// internal/api/mock/mock_queue.go
type MockQueueManager struct {
    mock.Mock
    StatsError     error
    EntriesError   error
    RetryError     error
    DeleteError    error
    StatsResult    *queue.Stats
    EntriesResult  []*queue.Entry
}

func (m *MockQueueManager) GetStats() (*queue.Stats, error) {
    args := m.Called()
    if m.StatsError != nil {
        return nil, m.StatsError
    }
    return m.StatsResult, nil
}

// ... diğer metodlar
```

### 1.3 Server Struct Güncellemesi

```go
// internal/api/server.go
type Server struct {
    // ... existing fields ...
    
    // Interface'ler (test edilebilirlik için)
    queueMgr   QueueManager
    msgStore   MessageStore
    pushSvc    PushService
    vacationMgr VacationManager
    filterMgr   FilterManager
    
    // Embed.FS için abstraction
    webmailFS  fs.FS
    adminFS    fs.FS
}
```

### 1.4 Constructor Güncellemesi

```go
// NewServerWithInterfaces - test için interface'li constructor
func NewServerWithInterfaces(
    db *db.DB,
    logger *slog.Logger,
    config Config,
    queueMgr QueueManager,
    msgStore MessageStore,
    pushSvc PushService,
    vacationMgr VacationManager,
    filterMgr FilterManager,
    webmailFS fs.FS,
    adminFS fs.FS,
) *Server {
    return &Server{
        db:          db,
        logger:      logger,
        config:      config,
        queueMgr:    queueMgr,
        msgStore:    msgStore,
        pushSvc:     pushSvc,
        vacationMgr: vacationMgr,
        filterMgr:   filterMgr,
        webmailFS:   webmailFS,
        adminFS:     adminFS,
    }
}
```

### 1.5 Test Dosyası Yapısı

```
internal/api/
├── interfaces.go              # Interface tanımları
├── server.go                  # Mevcut server (güncellenmiş)
├── vacation.go                # Mevcut (güncellenmiş)
├── push.go                    # Mevcut (güncellenmiş)
├── filters.go                 # Mevcut (güncellenmiş)
├── mock/
│   ├── mock_queue.go          # QueueManager mock
│   ├── mock_storage.go        # MessageStore mock
│   ├── mock_push.go           # PushService mock
│   ├── mock_vacation.go       # VacationManager mock
│   ├── mock_filter.go         # FilterManager mock
│   └── mock_fs.go             # fs.FS mock
└── tests/
    ├── server_test.go         # Interface-based tests
    ├── vacation_mock_test.go  # Vacation error path tests
    ├── push_mock_test.go      # Push error path tests
    └── filter_mock_test.go    # Filter error path tests
```

---

## Faz 2: Embed.FS Mock Sistemi

### 2.1 Webmail/Admin FS Mock

```go
// internal/api/mock/mock_fs.go

type MockFS struct {
    Files map[string]string
    OpenError map[string]error
}

func (m *MockFS) Open(name string) (fs.File, error) {
    if err, ok := m.OpenError[name]; ok {
        return nil, err
    }
    content, ok := m.Files[name]
    if !ok {
        return nil, fs.ErrNotExist
    }
    return &mockFile{content: content, name: name}, nil
}

// Test: internal/api/webmail_admin_test.go
func TestHandleWebmail_FileNotFound(t *testing.T) {
    mockFS := &MockFS{
        Files: map[string]string{
            "index.html": "<html></html>",
        },
        OpenError: map[string]error{
            "index.html": errors.New("mock fs error"),
        },
    }
    
    server := NewServerWithInterfaces(
        db, logger, config,
        nil, nil, nil, nil, nil,
        mockFS, nil,
    )
    
    req := httptest.NewRequest(http.MethodGet, "/webmail/", nil)
    rec := httptest.NewRecorder()
    server.ServeHTTP(rec, req)
    
    assert.Equal(t, http.StatusInternalServerError, rec.Code)
}
```

---

## Faz 3: SSE Auth Test Sistemi

### 3.1 SSE Test Endpoint

```go
// internal/api/sse_test_helper.go

// TestSSEAuth creates a testable SSE auth scenario
func TestSSEAuth(t *testing.T) {
    server, db, token := helperSetupAccount(t)
    defer db.Close()
    
    // Test valid token
    req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
    req.Header.Set("Authorization", "Bearer "+token)
    rec := httptest.NewRecorder()
    server.ServeHTTP(rec, req)
    
    // Test invalid token algorithm
    invalidToken := createTokenWithWrongAlg("test@example.com", true)
    req2 := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
    req2.Header.Set("Authorization", "Bearer "+invalidToken)
    rec2 := httptest.NewRecorder()
    server.ServeHTTP(rec2, req2)
    
    assert.Equal(t, http.StatusUnauthorized, rec2.Code)
}
```

---

## Faz 4: Config Modülü Refactoring

### 4.1 Config Validation Testleri

```go
// internal/config/validation_test.go

func TestConfigValidation_InvalidSize(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        wantErr bool
    }{
        {"empty", "", true},
        {"invalid", "not-a-size", true},
        {"negative", "-1MB", true},
        {"valid", "10MB", false},
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            var s Size
            err := s.UnmarshalYAML(func(v interface{}) error {
                *v.(*string) = tt.input
                return nil
            })
            if (err != nil) != tt.wantErr {
                t.Errorf("UnmarshalYAML() error = %v, wantErr %v", err, tt.wantErr)
            }
        })
    }
}
```

---

## Faz 5: JMAP Modülü Refactoring

### 5.1 JMAP Method Mock

```go
// internal/jmap/mock.go

type MockMethodCall struct {
    Response map[string]interface{}
    Error    error
}

type MockJMAPServer struct {
    Methods map[string]MockMethodCall
}

func (m *MockJMAPServer) HandleMethod(name string, args map[string]interface{}) (map[string]interface{}, error) {
    call, ok := m.Methods[name]
    if !ok {
        return nil, fmt.Errorf("unknown method: %s", name)
    }
    return call.Response, call.Error
}
```

---

## Faz 6: CalDAV/CardDAV Modülü Refactoring

### 6.1 Storage Interface

```go
// internal/caldav/interfaces.go

type Storage interface {
    CreateCalendar(user string, cal *Calendar) error
    GetCalendar(user, calID string) (*Calendar, error)
    UpdateCalendar(user string, cal *Calendar) error
    DeleteCalendar(user, calID string) error
    ListCalendars(user string) ([]*Calendar, error)
    
    SaveEvent(user, calID string, event *CalendarEvent, data string) error
    GetEvent(user, calID, eventID string) (*CalendarEvent, error)
    DeleteEvent(user, calID, eventID string) error
    ListEvents(user, calID string) ([]*CalendarEvent, error)
}
```

---

## Test Coverage Hedef Matrisi

| Modül | Mevcut | Hedef | Artış | Metod |
|-------|--------|-------|-------|-------|
| internal/api | 87.1% | 92% | +4.9% | Interface mock + embed.FS mock |
| internal/config | 86.7% | 90% | +3.3% | Validation testleri |
| internal/jmap | 86.7% | 90% | +3.3% | Method mock |
| internal/vacation | 87.1% | 90% | +2.9% | Manager interface |
| internal/carddav | 87.3% | 90% | +2.7% | Storage interface |
| internal/webhook | 87.8% | 90% | +2.2% | HTTP client mock |
| internal/auth | 88.0% | 90% | +2.0% | DNS mock |
| internal/push | 88.7% | 90% | +1.3% | Service interface |
| internal/circuitbreaker | 89.9% | 90% | +0.1% | State testleri |

---

## Implementasyon Sırası

### Sprint 1: Temel Interface Sistemi (2 hafta)
- [ ] `internal/api/interfaces.go` oluştur
- [ ] Mock implementasyonları yaz
- [ ] `NewServerWithInterfaces()` constructor ekle
- [ ] 5 temel test yaz (vacation, push, filter error path)

### Sprint 2: API Modülü Tamamlama (2 hafta)
- [ ] embed.FS mock sistemi
- [ ] SSE auth testleri
- [ ] Queue/MessageStore mock testleri
- [ ] API coverage 87.1% → 92%

### Sprint 3: Config/JMAP (1 hafta)
- [ ] Config validation testleri
- [ ] JMAP method mock
- [ ] Config 86.7% → 90%
- [ ] JMAP 86.7% → 90%

### Sprint 4: CalDAV/CardDAV (1 hafta)
- [ ] Storage interface
- [ ] Mock testleri
- [ ] CalDAV 85.1% → 90%
- [ ] CardDAV 87.3% → 90%

### Sprint 5: Temizlik ve Dokümantasyon (1 hafta)
- [ ] Kod review
- [ ] Mock kullanım dokümantasyonu
- [ ] CI/CD coverage check ekleme

**Toplam Süre:** 7 hafta (1 geliştirici)

---

## Riskler ve Mitigasyon

### Risk 1: Interface Değişiklikleri Mevcut Kodu Bozabilir
**Mitigasyon:** 
- Önce sadece test dosyalarında kullan
- Mevcut constructor'ları koru
- `NewServerWithInterfaces()` ayrı constructor olarak ekle

### Risk 2: Mock Bakımı Zor Olabilir
**Mitigasyon:**
- mockery veya mockgen gibi auto-generate araçları kullan
- CI'da mock generate check ekle

### Risk 3: Embed.FS Mock Karmaşık
**Mitigasyon:**
- `testing/fstest` paketini kullan
- Sadece gerekli metodları mock'la

### Risk 4: Test Süresi Artabilir
**Mitigasyon:**
- Parallel test kullan
- `-short` flag ile hızlı test modu

---

## CI/CD Entegrasyonu

```yaml
# .github/workflows/coverage.yml
coverage:
  runs-on: ubuntu-latest
  steps:
    - uses: actions/checkout@v4
    
    - name: Run tests with coverage
      run: go test -coverprofile=coverage.out ./...
    
    - name: Check API coverage
      run: |
        API_COVERAGE=$(go tool cover -func=coverage.out | grep "internal/api" | grep "total:" | awk '{print $3}' | tr -d '%')
        if (( $(echo "$API_COVERAGE < 90" | bc -l) )); then
          echo "API coverage $API_COVERAGE% is below 90%"
          exit 1
        fi
    
    - name: Upload coverage
      uses: codecov/codecov-action@v3
```

---

## Sonuç

Bu refactoring planı ile:
- Tüm modüller 90%+ coverage'a ulaşacak
- 3 modül 100% coverage'da kalacak
- Test edilebilirlik artacak
- Yeni özellikler için mock kullanımı kolaylaşacak

**Başarı Kriteri:**
```
make test-coverage
# Tüm modüller >= 90% coverage
# API modülü >= 92% coverage
```

---

## Referanslar

1. [Go Testing Patterns](https://github.com/google/go-cloud/blob/master/internal/testing/octest/doc.go)
2. [Mock Interfaces](https://github.com/uber-go/mock)
3. [Embed.FS Testing](https://pkg.go.dev/testing/fstest)
4. [Test Coverage Best Practices](https://dave.cheney.net/2019/04/29/try-harder-to-write-tests-that-fail-when-the-code-changes)
