# 수정된 데이터베이스 적용 방법

운영 환경으로 옮겨야 하는 파일은 `javinizer.db` 하나입니다.

## 적용 순서

1. Javinizer API, Web UI 및 배치 작업을 모두 종료합니다.
2. 운영 환경에 있는 기존 `javinizer.db`를 별도 위치에 백업합니다.
3. 이 디렉터리의 `javinizer.db`를 운영 환경에서 실제로 사용하는 데이터베이스 경로에 덮어씁니다.
4. 같은 경로에 이전 실행에서 남은 `javinizer.db-wal` 또는 `javinizer.db-shm`이 있다면, Javinizer가 완전히 종료된 상태에서 제거합니다.
5. Javinizer를 다시 시작한 뒤 `/actress`와 organize 대기 작업을 확인합니다.

`*.backup`, 로그, 재처리 목록, `*.lock`, `javinizer.db-wal`, `javinizer.db-shm` 파일은 옮기지 않습니다. 현재 `javinizer.db`에는 이번에 직접 보정한 배우·번역·대기 작업 데이터가 반영되어 있습니다.
