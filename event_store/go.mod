module github.com/solidtrace/event_store

go 1.27.3

require (
	cloud.google.com/go/storage v1.62.1
	github.com/ClickHouse/clickhouse-go/v2 v2.49.0
	github.com/andybalholm/brotli v1.2.6
	github.com/aws/aws-sdk-go-v2 v1.41.7
	github.com/aws/aws-sdk-go-v2/config v1.32.17
	github.com/aws/aws-sdk-go-v2/credentials v1.19.16
	github.com/aws/aws-sdk-go-v2/service/s3 v1.101.0
	github.com/aws/smithy-go v1.25.1
	github.com/cockroachdb/pebble/v2 v2.1.7
	github.com/gofiber/fiber/v2 v2.52.14
	github.com/google/uuid v1.6.0
	github.com/klauspost/compress v1.19.1
	github.com/mattn/go-sqlite3 v1.14.33
	github.com/shopspring/decimal v1.4.0
	github.com/valyala/fasthttp v1.51.0
	github.com/vmihailenco/msgpack/v5 v5.4.1
	github.com/zerodha/logf v0.5.5
	golang.org/x/sys v0.48.0
	google.golang.org/api v0.278.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	cel.dev/expr v0.25.2 // indirect
	cloud.google.com/go v0.123.0 // indirect
	cloud.google.com/go/auth v0.20.0 // indirect
	cloud.google.com/go/auth/oauth2adapt v0.2.8 // indirect
	cloud.google.com/go/compute/metadata v0.9.0 // indirect
	cloud.google.com/go/iam v1.7.0 // indirect
	cloud.google.com/go/monitoring v1.24.3 // indirect
	github.com/ClickHouse/ch-go v0.74.0 // indirect
	github.com/DataDog/zstd v1.5.7 // indirect
	github.com/GoogleCloudPlatform/opentelemetry-operations-go/detectors/gcp v1.33.0 // indirect
	github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/metric v0.55.0 // indirect
	github.com/GoogleCloudPlatform/opentelemetry-operations-go/internal/resourcemapping v0.55.0 // indirect
	github.com/RaduBerinde/axisds v0.1.0 // indirect
	github.com/RaduBerinde/btreemap v0.0.0-20250419174037-3d62b7205d54 // indirect
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.7.10 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.18.23 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.4.23 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.7.23 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.4.24 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.9 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/checksum v1.9.15 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.13.23 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/s3shared v1.19.23 // indirect
	github.com/aws/aws-sdk-go-v2/service/signin v1.0.11 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.30.17 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.35.21 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.42.1 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/cncf/xds/go v0.0.0-20260202195803-dba9d589def2 // indirect
	github.com/cockroachdb/crlib v0.0.0-20241112164430-1264a2edc35b // indirect
	github.com/cockroachdb/errors v1.11.3 // indirect
	github.com/cockroachdb/logtags v0.0.0-20230118201751-21c54148d20b // indirect
	github.com/cockroachdb/redact v1.1.5 // indirect
	github.com/cockroachdb/swiss v0.0.0-20260820225851-333444432258 // indirect
	github.com/cockroachdb/tokenbucket v0.0.0-20230807174530-cc333fc44b06 // indirect
	github.com/envoyproxy/go-control-plane/envoy v1.37.0 // indirect
	github.com/envoyproxy/protoc-gen-validate v1.3.3 // indirect
	github.com/felixge/httpsnoop v1.1.0 // indirect
	github.com/getsentry/sentry-go v0.27.0 // indirect
	github.com/go-faster/city v1.0.1 // indirect
	github.com/go-faster/errors v0.7.1 // indirect
	github.com/go-jose/go-jose/v4 v4.1.4 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/golang/snappy v1.0.0 // indirect
	github.com/google/s2a-go v0.1.9 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.15 // indirect
	github.com/googleapis/gax-go/v2 v2.22.0 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mattn/go-runewidth v0.0.16 // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.4 // indirect
	github.com/minio/minlz v1.0.1-0.20250507153514-87eb42fe8882 // indirect
	github.com/paulmach/orb v0.13.0 // indirect
	github.com/pierrec/lz4/v4 v4.1.27 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/planetscale/vtprotobuf v0.6.1-0.20240319094008-0393e58bdf10 // indirect
	github.com/prometheus/client_golang v1.16.0 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.42.0 // indirect
	github.com/prometheus/procfs v0.10.1 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	github.com/segmentio/asm v1.2.1 // indirect
	github.com/spiffe/go-spiffe/v2 v2.7.0 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/tcplisten v1.0.0 // indirect
	github.com/vmihailenco/tagparser/v2 v2.0.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/detectors/gcp v1.44.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.67.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.69.0 // indirect
	go.opentelemetry.io/otel v1.46.0 // indirect
	go.opentelemetry.io/otel/metric v1.46.0 // indirect
	go.opentelemetry.io/otel/sdk v1.45.0 // indirect
	go.opentelemetry.io/otel/sdk/metric v1.45.0 // indirect
	go.opentelemetry.io/otel/trace v1.46.0 // indirect
	golang.org/x/crypto v0.57.0 // indirect
	golang.org/x/exp v0.0.0-20260112195511-716be5621a96 // indirect
	golang.org/x/net v0.60.0 // indirect
	golang.org/x/oauth2 v0.36.0 // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/text v0.42.0 // indirect
	golang.org/x/time v0.15.0 // indirect
	google.golang.org/genproto v0.0.0-20260319201613-d00831a3d3e7 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260526163538-3dc84a4a5aaa // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa // indirect
	google.golang.org/grpc v1.83.2 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)
