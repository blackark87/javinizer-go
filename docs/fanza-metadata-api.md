# fanza-mcp Metadata HTTP API 명세

이 문서는 fanza-mcp `0.10.0`의 캐시 메타데이터·미디어 조회와 소비 완료
계약을 정의한다. OpenClaw는 이 API를 사용하지 않는다. 같은 Docker 내부
네트워크의 메타데이터 소비자가 fanza-mcp에서 데이터를 가져갈 때 사용한다.

## 연결 및 공통 규칙

- 기본 Base URL: `http://fanza-mcp:8000`
- API 버전: `v1`
- 인증: 없음
- 네트워크: fanza-mcp와 소비자가 함께 연결된 내부 `vpn_network`
- 프록시: 내부 fanza-mcp 요청에는 사용하지 않음
- JSON 응답 Content-Type: `application/json`
- 품번은 `ABC-001` 형식으로 전달하며 응답에는 정규화된 대문자 품번을 반환함

이 API는 이미 수집된 캐시와 로컬 미디어만 제공한다. API 요청 자체는 FANZA,
LibreDMM 또는 Chromium을 호출하지 않는다. 캐시가 없거나 만료됐으면 외부에서
다시 수집하지 않고 `404`를 반환한다.

## 권장 호출 순서

미디어가 필요 없는 사전 확인:

```text
GET metadata?dry_run=true
└─ 메타데이터만 사용
```

메타데이터와 미디어를 실제로 가져가는 경우:

```text
GET metadata
├─ 응답의 미디어 URL을 필요한 만큼 GET
└─ 모든 미디어 처리가 끝난 뒤 POST consume
   └─ 메타데이터 DB 행과 로컬 미디어 즉시 삭제
```

`consume`을 호출하지 않으면 메타데이터와 미디어는 기존 보존 정책에 따라
수집 시각부터 10일 후 자동 삭제된다.

## 메타데이터 조회

```http
GET /api/v1/metadata/{product_code}?dry_run={true|false}
Accept: application/json
```

### Query parameter

| 이름 | 필수 | 기본값 | 설명 |
|---|---:|---|---|
| `dry_run` | 아니요 | `false` | `true`이면 메타데이터만 반환하고 미디어 및 소비 완료 URL을 제공하지 않음 |

`dry_run`은 대소문자 구분 없이 `true` 또는 `false`만 허용한다. 다른 값은
`400`을 반환한다.

### 일반 조회 성공 응답

`dry_run=false`이거나 query parameter를 생략한 경우다.

```json
{
  "source": "fanza-mcp",
  "source_url": "https://video.dmm.co.jp/av/content/?id=abc00001",
  "language": "ja",
  "id": "ABC-001",
  "content_id": "abc00001",
  "title": "원문 제목",
  "original_title": "원문 제목",
  "description": "작품 설명",
  "release_date": "2026-07-28",
  "runtime": 132,
  "director": "テスト監督",
  "maker": "テストメーカー",
  "label": "テストレーベル",
  "series": "テストシリーズ",
  "rating": {
    "score": 8.0
  },
  "actresses": [
    {
      "dmm_id": 123456,
      "first_name": "",
      "last_name": "",
      "japanese_name": "宮下玲奈",
      "reading": "",
      "thumb_url": ""
    }
  ],
  "genres": ["単体作品", "ハイビジョン"],
  "poster_url": "http://fanza-mcp:8000/api/v1/media/ABC-001/poster",
  "cover_url": "http://fanza-mcp:8000/api/v1/media/ABC-001/cover",
  "should_crop_poster": false,
  "screenshot_urls": [
    "http://fanza-mcp:8000/api/v1/media/ABC-001/screenshots/1"
  ],
  "trailer_url": "http://fanza-mcp:8000/api/v1/media/ABC-001/trailer",
  "providers": ["fanza", "libredmm"],
  "fetched_at": 1785196800.0,
  "expires_at": 1786060800.0,
  "dry_run": false,
  "consume_url": "http://fanza-mcp:8000/api/v1/metadata/ABC-001/consume"
}
```

미디어 URL은 해당 로컬 파일이 있을 때만 채워진다. 값이 비어 있으면 그 자산은
요청하지 않는다. `consume_url`은 일반 조회에만 제공된다.

### Dry-run 성공 응답

`dry_run=true`는 캐시된 메타데이터를 확인하기 위한 비파괴 조회다.

- 외부 메타데이터 공급자를 호출하지 않음
- 미디어 파일을 전송하지 않음
- 메타데이터나 로컬 미디어를 삭제하지 않음
- `poster_url`, `cover_url`, `trailer_url`, `consume_url`은 빈 문자열
- `screenshot_urls`는 빈 배열
- `dry_run`은 `true`

나머지 메타데이터 필드는 일반 조회와 동일하다. 승인 시점에 이미 저장된
미디어가 있더라도 dry-run 응답에는 그 URL을 노출하지 않는다.

### 필드 계약

| 필드 | 타입 | 값이 없을 때 | 설명 |
|---|---|---|---|
| `source` | string | 항상 `fanza-mcp` | 메타데이터 공급 서비스 |
| `source_url` | string | `""` | 원본 작품 페이지 |
| `language` | string | 항상 `ja` | 원문 언어 |
| `id` | string | 필수 | 정규화된 품번 |
| `content_id` | string | 필수 | 공급자 content ID |
| `title` | string | 필수 | 원문 제목 |
| `original_title` | string | 필수 | 현재 `title`과 동일 |
| `description` | string | `""` | 작품 설명 |
| `release_date` | string 또는 null | `null` | JST/KST 달력 날짜 `YYYY-MM-DD` |
| `runtime` | integer | `0` | 재생 시간(분) |
| `director` | string | `""` | 감독 |
| `maker` | string | `""` | 제작사 |
| `label` | string | `""` | 레이블 |
| `series` | string | `""` | 시리즈 |
| `rating` | object 또는 null | `null` | 존재하면 `{"score": number}` |
| `actresses` | array | `[]` | 출연자 객체 목록 |
| `genres` | string array | `[]` | 장르 목록 |
| `poster_url` | string | `""` | 포스터 API URL |
| `cover_url` | string | `""` | 표지 API URL |
| `should_crop_poster` | boolean | `false` | 포스터 대신 표지를 세로로 잘라야 하는지 여부 |
| `screenshot_urls` | string array | `[]` | 1부터 시작하는 스크린샷 API URL 목록 |
| `trailer_url` | string | `""` | 트레일러 API URL |
| `providers` | string array | `[]` | 캐시 구성에 사용된 공급자 |
| `fetched_at` | number | `0.0` | Unix timestamp |
| `expires_at` | number | `0.0` | 자동 삭제 예정 Unix timestamp |
| `dry_run` | boolean | 필수 | 요청에 적용된 dry-run 여부 |
| `consume_url` | string | `""` | 일반 조회 후 소비 완료를 알릴 POST URL |

`actresses[]` 객체는 다음 필드를 항상 가진다.

| 필드 | 타입 | 값이 없을 때 |
|---|---|---|
| `dmm_id` | integer | `0` |
| `first_name` | string | `""` |
| `last_name` | string | `""` |
| `japanese_name` | string | `""` |
| `reading` | string | `""` |
| `thumb_url` | string | `""` |

### 메타데이터 조회 상태 코드

| 상태 | 응답 |
|---:|---|
| `200` | 메타데이터 JSON |
| `400` | `{"error":"invalid product code"}` |
| `400` | `{"error":"dry_run must be true or false"}` |
| `404` | `{"error":"metadata not found"}` |
| `503` | `{"error":"metadata service unavailable"}` |

## 미디어 조회

일반 메타데이터 응답이 반환한 URL만 사용한다.

```http
GET|HEAD /api/v1/media/{product_code}/poster
GET|HEAD /api/v1/media/{product_code}/cover
GET|HEAD /api/v1/media/{product_code}/screenshots/{index}
GET|HEAD /api/v1/media/{product_code}/trailer
```

- `screenshots/{index}`는 1부터 시작한다.
- `GET`은 파일 본문을 반환한다.
- `HEAD`는 본문 없이 같은 파일 헤더를 반환한다.
- 이미지 Content-Type은 저장 파일에 따라 `image/jpeg`, `image/png` 또는
  `image/webp`다.
- 트레일러 Content-Type은 `video/mp4`다.
- URL에 확장자가 없으므로 저장 확장자가 필요하면 Content-Type을 기준으로 한다.

### 미디어 상태 코드

| 상태 | 응답 |
|---:|---|
| `200` | 바이너리 파일 또는 HEAD 헤더 |
| `404` | `{"error":"media not found"}` |
| `503` | `{"error":"metadata service unavailable"}` |

## 소비 완료

일반 메타데이터 조회에서 필요한 미디어를 모두 받은 뒤 호출한다.

```http
POST /api/v1/metadata/{product_code}/consume
Accept: application/json
```

요청 body는 없다. 이 요청은 즉시 다음 데이터를 삭제한다.

- `product_metadata`의 해당 품번 행
- `/data/media/{product_code}`에 해당하는 안전한 로컬 미디어 디렉터리

다음 데이터는 삭제하지 않는다.

- 다운로드 이력
- 배우 확정 정보
- 실행 watermark
- 승인된 `.torrent` 파일

`.torrent` 파일에는 별도의 기존 10일 보존 정책이 적용된다.

### 성공 응답

```json
{
  "status": "consumed",
  "product_code": "ABC-001",
  "metadata_deleted": true,
  "media_directory_deleted": true,
  "media_file_count": 3
}
```

| 필드 | 타입 | 설명 |
|---|---|---|
| `status` | string | 항상 `consumed` |
| `product_code` | string | 정규화된 품번 |
| `metadata_deleted` | boolean | 성공 응답에서는 `true` |
| `media_directory_deleted` | boolean | 삭제할 안전한 미디어 디렉터리가 존재했는지 여부 |
| `media_file_count` | integer | 삭제 직전에 디렉터리에 있던 일반 파일 수 |

성공 응답 이후 같은 품번의 메타데이터·미디어 조회와 반복 consume은 `404`다.
따라서 consume은 재시도 가능한 멱등 성공 API가 아니다. 응답을 받지 못해 결과를
확인해야 하면 메타데이터를 다시 조회하고 `404`를 이미 소비된 상태로 취급한다.

### 소비 완료 상태 코드

| 상태 | 응답 |
|---:|---|
| `200` | 소비 완료 JSON |
| `400` | `{"error":"invalid product code"}` |
| `404` | `{"error":"metadata not found"}` |
| `503` | `{"error":"metadata service unavailable"}` |

## 소비자 구현 요구사항

1. 응답의 `id`가 요청 품번과 같은 정규화 값인지 확인한다.
2. 알 수 없는 응답 필드는 무시해 하위 호환성을 유지한다.
3. 미디어 URL은 메타데이터 Base URL과 같은 scheme·authority인지 확인한다.
4. 미디어 path가 `/api/v1/media/{id}/` 아래인지 확인한다.
5. 내부 fanza-mcp 요청에는 환경 및 전역 다운로드 프록시를 적용하지 않는다.
6. 모든 필요한 미디어 요청이 끝나기 전에는 `consume`을 호출하지 않는다.
7. 일부 선택 미디어가 `404`이면 그 자산을 생략한 뒤 나머지 처리를 계속할 수
   있다.
8. 메타데이터 또는 필수 미디어 처리가 실패했다면 consume을 호출하지 않고
   10일 자동 삭제에 맡긴다.
