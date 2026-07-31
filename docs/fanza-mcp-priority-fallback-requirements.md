# FANZA MCP 우선 scraper 및 누락 필드 fallback 요구사항

## 목표

- `fanzamcp`를 일반 scraper로 등록하고 운영 설정의 `scrapers.priority` 첫
  번째에 배치한다.
- 코드에서 `fanzamcp`를 특별 취급하거나 항상 첫 번째로 강제하지 않는다.
- 현재 설정된 priority 순서대로 scraper를 조회하고, 상위 scraper가 제공한
  값은 유지하면서 비어 있는 필드만 하위 scraper 결과로 보충한다.
- FANZA MCP 캐시가 충분한 작품은 외부 scraper 호출을 가능한 한 줄인다.

## MDVR-338 확인 결과

FANZA MCP `GET /api/v1/metadata/MDVR-338`과 Javinizer 격리 scrape를 기준으로
확인했다.

### 바로 사용할 수 있는 데이터

- ID: `MDVR-338`
- Content ID: `mdvr00338`
- 일본어 제목과 원제
- 고해상도 포스터: `1605x2184`
- 고해상도 커버: `2184x1638`
- 스크린샷 URL 12개
- 원본 DMM 상세 URL

### 하위 scraper 보충이 필요한 데이터

- 설명
- 출시일
- 런타임
- 감독
- 메이커
- 라벨
- 시리즈
- 평점
- 장르
- 트레일러

FANZA MCP 응답의 배우 목록은 비어 있었다. Javinizer의 SougouWiki 배우
resolver가 `宮下玲奈`와 DMM ID `1075464`를 보충했지만, FANZA MCP만으로
완결되는 데이터는 아니다.

### 데이터 품질 후속 항목

- 첫 번째 screenshot은 실제 장면이 아니라 포스터의 축소 복제본이다.
  Javinizer adapter는 URL만으로 이미지 중복을 판정할 수 없으므로, 우선
  fanza-mcp의 `screenshot_urls` 생성 단계에서 poster 중복을 제외하는 것이
  적절하다.
- 현재 fanza-mcp는 `rating`을 항상 `null`로 반환한다. Javinizer는 향후
  객체 값이 제공될 때 사용할 수 있도록 응답 필드를 매핑한다.

## 설정 계약

운영 설정에서만 다음과 같이 활성화한다. 저장소 기본값이나 Go 코드에
`fanzamcp` 우선순위를 하드코딩하지 않는다.

```yaml
scrapers:
    early_stop: true
    early_stop_min_results: 1
    early_stop_fields:
        - id
        - content_id
        - title
        - original_title
        - description
        - release_date
        - runtime
        - maker
        - label
        - poster_url
        - cover_url
        - screenshots
        - actresses
        - genres
    priority:
        - fanzamcp
        # 나머지는 기존 운영 priority 순서를 유지한다.
        - dmm
        - fc2
        - libredmm

    fanzamcp:
        enabled: true
        base_url: "http://fanza-mcp:8000"
        rate_limit: 0
        timeout: 30
```

`early_stop_fields`는 추가 scraper 호출을 멈출 수 있는 데이터 완성 기준이며,
`metadata.required_fields`와 다르다. 모든 scraper를 조회한 뒤에도 선택 필드가
비어 있을 수 있지만 이것만으로 scrape 전체를 실패시키지 않는다.

감독, 시리즈, 평점, 트레일러처럼 작품에 따라 정상적으로 존재하지 않을 수
있는 필드는 기본 완성 기준에서 제외한다. 이미 다른 필드를 보충하기 위해
호출된 scraper가 이 값을 제공하면 기존 priority 규칙에 따라 함께 채운다.
운영자가 이 필드 때문에도 다음 scraper를 호출하려면 `early_stop_fields`에
명시적으로 추가할 수 있다.

`metadata.priority`의 필드별 값이 `[]`이면 전역 `scrapers.priority`를
상속한다. 비어 있지 않은 목록은 해당 필드에서만 사용하는 배타적 override이므로
필요한 경우가 아니면 설정하지 않는다.

## 처리 순서

1. 설정된 `scrapers.priority`의 첫 번째 활성 scraper를 조회한다.
2. 성공 결과를 누적하고 `early_stop_fields`의 데이터 충족 여부를 확인한다.
3. 부족한 필드가 있으면 다음 활성 scraper를 조회한다.
4. 완성 기준을 충족하면 남은 하위 scraper는 호출하지 않는다.
5. aggregator는 각 스칼라 필드와 장르·스크린샷에 대해 우선순위상 첫 번째
   유효 값을 사용한다.
6. 모든 scraper가 끝나도 선택 필드가 비어 있으면 빈 값으로 허용한다.

배우 목록은 현재 여러 공급자 결과를 병합·중복 제거하는 별도 정책을 유지한다.
FANZA MCP가 검증된 전체 배우 목록을 안정적으로 제공하게 되면 상위 scraper
목록을 권위 있는 단일 목록으로 취급할지 별도로 결정한다.

## Javinizer 코드 보완 항목

- FANZA MCP 응답의 `original_title`을 `ScraperResult.OriginalTitle`로 매핑하고,
  비어 있을 때만 `title`을 fallback으로 사용한다.
- `runtime`, `series`, `rating`을 `ScraperResult`에 매핑한다.
- `early_stop_fields`를 YAML/JSON 설정에 추가하고 scrape 계층으로 전달한다.
- `early_stop_fields`가 있으면 priority 순서로 순차 조회하고, 충족 시 남은
  scraper 호출을 중단한다.
- 기존 `metadata.required_fields`의 최종 검증·실패 의미는 변경하지 않는다.
- FANZA MCP가 첫 번째일 때 상위의 유효 값은 유지되고 빈 값만 하위 결과로
  채워지는 회귀 테스트를 추가한다.

## 구현 후 MDVR-338 fallback 확인

운영 DB 대신 `/tmp/javinizer-mdvr338/javinizer.db`를 사용하고 번역을 비활성화한
격리 설정에서 priority를 `fanzamcp → dmm → libredmm`으로 구성해 다시 조회했다.

- FANZA MCP 값 유지: ID, Content ID, 제목, 원제, 포스터, 커버, screenshot 12개
- 후순위에서 보충: 출시일 `2025-03-20`, 런타임 `132`, 감독 `濡れた子犬`,
  메이커 `ムーディーズ`, 라벨 `MOODYZ VR`, 평점 `10.0`, 배우 `宮下玲奈`, 장르
- 세 공급자 이후에도 빈 값: 설명, 시리즈, 트레일러
- 최종 `source_name`과 미디어 URL은 `fanzamcp`로 유지되었다.

이 결과는 상위 scraper 결과 전체를 후순위 결과로 덮는 방식이 아니라,
aggregator가 필드마다 priority상 첫 번째 유효 값만 선택하는 동작을 확인한다.
설명이 계속 비어 있었기 때문에 설정된 후보가 더 있다면 다음 scraper 조회가
계속되는 것이 의도한 동작이다.

## 검증 기준

- 설정 YAML/JSON round-trip에서 `early_stop_fields`가 보존된다.
- `early_stop_fields`가 없는 기존 설정은 종전 `required_fields` 기반
  early-stop 동작을 유지한다.
- 첫 scraper가 완성 기준을 충족하면 두 번째 scraper는 호출되지 않는다.
- 첫 scraper에 누락 필드가 있으면 다음 scraper를 호출한다.
- FANZA MCP 응답의 원제·런타임·시리즈·평점이 최종 Movie까지 전달된다.
- `MDVR-338` 격리 scrape에서 운영 DB와 `real-data/`를 수정하지 않는다.
