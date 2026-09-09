# Phase 5 Validation Manifest

**Tarih:** 2026-09-10<br>
**Branch:** `architecture-remediation`<br>
**Base:** `main` @ `2d833c2`<br>
**Durum:** tamamlandı

Bu belge, Phase 4 blueprint içindeki M4–M8 kartlarının uygulama ve doğrulama kanıtlarını tek yerde toplar. `_plan/` takip dosyası bilinçli olarak bu commit’in dışında bırakılmıştır.

## Kart kanıtları

| Kart | Kapsam | Commit kanıtı | Doğrulama |
| --- | --- | --- | --- |
| M4 | Tek runtime içinde sync → embed → vector akışı, collection-scoped pending set, context propagation | `d0354d2`, `f89c859`, `c9193e5` | Pipeline cancellation ve collection isolation testleri; tam FTS5 suite |
| M5 | RRF/rerank/content-kind/filter policy’sinin search domain içinde merkezileştirilmesi | `24671d3`, `c9193e5` | Search quality, sort error, repository ve aggregation regression testleri |
| M6 | Typed/context-aware source handler registry; format handler dosyalarının ayrıştırılması | `f89c859`, `e5b9b64`, `c9193e5` | Markdown, code, conversation, PDF/image, document ve parser indexer testleri |
| M7 | Provider capability bundle, config-driven factory/registry ve runtime injection | `c9193e5` | Provider factory contract/table testleri; embed/pipeline suite |
| M8 | Bu doğrulama manifesti ve kapanış kanıtları | `c9193e5` + bu belge | Aşağıdaki komut matrisi |

İlişkili persistence/lifecycle zemin commit’leri: `0ff1576` (transaction/index/vector lifecycle), `0f02ff9` (composition-root Store adapter).

## Uygulanan review düzeltmeleri

- Search üretim kodundan `database/sql`, `Store.DB()` ve SQL aggregation escape hatch’i kaldırıldı; aggregation testleri Store katmanına taşındı.
- Scan/parser/extraction/persistence, sort/rerank, vector lifecycle ve runtime close hataları görünür şekilde döndürülüyor veya raporlanıyor.
- Context source taramasından parser, indexer, embedding, Store ve search çağrılarına taşınıyor.
- HNSW eksik/corrupt/uyumsuz state’i SQLite embedding’lerinden rebuild ediyor; generation/manifest ve flush/close yolları testli.
- Indexer handler’ları ayrı format dosyalarına, Store operasyonları private repository seam’lerine ayrıldı.
- `FilterSet` mapping ve retrieval quality policy tekrarları kaldırıldı; provider capability eksikleri panic yerine açık hata veriyor.

## Doğrulama matrisi

```text
GOCACHE=/tmp/seek-gocache go test -tags fts5 ./...                                      PASS
GOCACHE=/tmp/seek-gocache go vet -tags fts5 ./...                                       PASS
GOCACHE=/tmp/seek-gocache CGO_ENABLED=1 go build -tags fts5 -o /dev/null .             PASS
test -z "$(gofmt -l cmd internal main.go third_party)" && git diff --check             PASS
GOCACHE=/tmp/seek-gocache go test -race -tags fts5 \
  ./internal/store ./internal/search ./internal/indexer ./internal/pipeline              PASS
```

Tam suite `cmd`, `app`, `embed`, `extractor`, `indexer`, `pipeline`, `search`, `source` ve `store` paketlerini kapsadı. Race suite Store, Search, Indexer ve Pipeline kapsamındadır.

## Test envanteri

- `internal/pipeline/degrade_test.go`: keyword-only degradation, collection-scoped embedding ve provider cancellation.
- `internal/embed/factory_test.go`: config-driven capability bundle ve registry contract.
- `internal/search/*_test.go`: fake repository, aggregation/filter boundary, RRF/rerank, content enrichment, cancellation ve sort error.
- `internal/store/aggregation_test.go`: Store-owned aggregation, whitelist/filter/scan error yolları.
- `internal/store/vector_index_test.go`: restart round-trip, missing/corrupt state, generation recovery ve flush lifecycle.
- `internal/indexer/*_test.go`: typed handler dispatch, context, failure accounting ve format sync regression’ları.
- `cmd/service_test.go`: üç platform service template’inde tek sync entrypoint ve duplicate embed yokluğu.

## Graph/review notu

GitNexus blast-radius kontrolü 53 staged dosya, 71 değişen sembol ve geniş command → store/search/indexer akışı tespit etti; sonuç `partial=false` idi. GitNexus indeksinin branch HEAD’ine göre stale olması ve codebase-memory FTS uyarısı nedeniyle graph çıktısı tamamlayıcı risk sinyali olarak kullanıldı; kritik akışlar kaynak incelemesi, gerçek SQLite testleri ve tam suite ile doğrulandı.
