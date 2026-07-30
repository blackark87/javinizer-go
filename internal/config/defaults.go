package config

import "github.com/javinizer/javinizer-go/internal/models"

const fallbackKoreanJAVDictionary = `JAV 문체 -> 성기·성행위·사정 표현은 원문의 노골성을 유지해 보지, 자지, 보빨, 대딸, 박다, 싸다처럼 직설적으로 번역. 소중이, 그곳, 중요 부위 같은 완곡어로 순화하지 않음
문맥 원칙 -> 비성적 일반어를 억지로 음란하게 만들지 않되, 성적 중의어는 JAV 문맥을 우선
中出し -> 질내사정(제목·장르) / 안에 싸다(거친 대사·서술)
手コキ / ハンドジョブ / handjob -> 대딸
ナックル手コキ -> 손가락 대딸. 너클 대딸 / 손가락 마디를 이용한 대딸처럼 직역하거나 설명하지 않음
フェラ / フェラチオ -> 펠라(제목·장르) / 자지 빨기(거친 행위 묘사)
クンニ / クンニリングス -> 보빨
シックスナイン / 69 / ６９ -> 69
素股 -> 가랑이딸
パイパン -> 백보지
美脚 -> 각선미
美乳 -> 예쁜 가슴
巨乳 -> 거유
爆乳 -> 폭유
美巨乳 -> 예쁜 거유. 미거유로 음차하지 않음
ちっぱい -> 빈유(컵 크기·신체 라벨) / 작은 가슴(자연스러운 서술). 치っぱい처럼 일부만 번역하지 않음
爆美女 -> 초미녀. 폭녀로 번역하지 않음
桃尻 / 桃Siri -> 애플힙
白桃尻 -> 뽀얀 애플힙
まんこ / マンコ / おま〇こ -> 보지
ツルツルパイパンの綺麗なマ●コ -> 매끈하고 예쁜 백보지
チンポ / ちんぽ -> 자지
潮吹き -> 분수 / 분수 폭발
潮を部屋中に大噴出 -> 방 안 가득 분수 폭발
潮吹き処女も頂いちゃった模様 -> 생애 첫 분수까지 터뜨려버린 듯
ごっくん -> 정액 삼키기 / 정액을 삼키다. 단순 꿀꺽으로 번역하지 않음
イラマチオ / イラマ -> 이라마치오
オホ声 -> 거친 신음
ちゃん / たん -> 이름 뒤의 호칭 접미사일 때 짱. 검증된 배우 활동명의 일부라면 분리하거나 의미 번역하지 않음
にゃんこちゃん -> 야옹이짱 또는 고양이짱. 야옹이 ちゃん처럼 일본어를 남기지 않음
ドM -> 극M
即尺 -> 바로 빨기
即ハメ -> 바로 박기
手マン -> 핑거링(제목·장르) / 보지를 손가락으로 쑤시다(거친 서술)
ガシガシ手マン -> 거친 핑거링 / 보지를 손가락으로 거칠게 쑤시다
激ピス -> 격렬한 피스톤
猛ピス -> 거친 피스톤 / 피스톤 맹공. 피스토닉처럼 음차하지 않음
電クリ -> 전동 클리 자극
電マ -> 전마
電マ自慰 -> 전마 자위. 금지: 전마 자위(전동 마사지기 자위) / 전동 마사지기 자위 / 괄호 설명 덧붙이기
ずぽずぽデート -> 푹푹 박는 데이트. 금지: 즈보즈로 데이트 / 즈보즈보 데이트 / 깊숙한 피스톤 데이트
パコ撮り -> 섹스 촬영. No. 등의 뒤따르는 번호는 그대로 유지. 금지: 파코 촬영
鬼イカセ -> 무자비 강제 절정. 연속 표현은 원문에 연속성이 있을 때만 사용
ガン突き -> 쑤셔박기
激イキ -> 격렬 절정
大放出 -> 분수 대방출 / 정액 대방출. 방출 대상을 원문 문맥에서 판단
ガン突き激イキ大放出セックス -> 쑤셔박기·격렬 절정·분수 대방출 섹스
ハメる / ハメられる -> 박다 / 박히다
挿れる -> 박아 넣다
射精 -> 사정 / 싸다
ザーメン / 精液 / 精子 -> 정액
膣in / 膣イン -> 질 삽입. 질내사정으로 행위를 바꾸지 않음
生挿入 -> 노콘 삽입
生中 -> 노콘 질내사정. 생중계로 번역하지 않음
ケツマンコ -> 후장
アナル -> 애널
性獣 -> 색마
ヤリモク -> 섹스만 노리는. 야리모쿠로 음차하지 않음
股コキ -> 가랑이딸
太ももコキ -> 허벅지딸
尻コキ -> 엉덩이딸
おっパブ -> 옵파이 펍 또는 슴가 펍
ハメ撮り -> POV 섹스 / 셀프 섹스 촬영. 일반 셀프카메라로 순화하지 않음
ハメ撮り映像流出 -> 셀프 섹스 촬영 영상 유출. 영상 유무로 바꾸지 않음
秘蔵 -> 미공개 또는 비공개 소장 (영상 홍보 문맥)
蔵出し / 蔵出し動画 -> 미공개 영상 또는 소장 영상 공개
ガルバ -> 걸즈바
気弱 -> 소심한
床上手 -> 섹스 고수
レロレロ -> 레로레로
猫じゃらし -> 고양이 장난감
現役J● -> 현역 J●. 1인칭 J●로 번역하지 않음
尾行押し込み -> 범죄 문맥에서 미행·주거침입. 미행 후 몰아붙이기로 번역하지 않음
ヤリサー -> 섹스 동아리 / 섹스 서클
おっぱいちゃん -> 성인 여성 문맥에서 거유녀 / 가슴이 큰 여자. 가슴짱으로 직역하지 않음
ストゼロガンギマリ -> 스트롱 제로에 완전히 취한. 술을 약기운으로 바꾸지 않음
マジ軟派、初撮。 -> 진짜 헌팅, 첫 촬영. 진짜 난파로 번역하지 않음
連れ込みSEX隠し撮り -> 데려와 섹스 몰카
夜の蝶 -> JAV 유흥업소 문맥에서 캬바걸 / 캬바클럽 호스티스
キレイ系 -> 미인형. 청순한으로 의미를 바꾸지 않음
隠れた -> 숨은 / 숨겨진. 숨겨된 같은 손상 문자열을 만들지 않음
1年ぶり -> 1년 만의. 年 단위를 누락하지 않음
クラスの女子が1人で -> 반 여학생이 혼자. 반 여자애들이 1명이나로 번역하지 않음
ガチイキ -> 진짜 절정
白ムチ -> 하얗고 통통한
絶品 -> 최고의. 절품으로 음차하지 않음
カメコ -> 코스프레 문맥에서 코스프레 촬영자
嫌われた底辺カメコ -> 기피당하는 밑바닥 코스프레 촬영자
18歳のパイパンボディ -> 18세의 백보지. 백보지 몸매처럼 부자연스럽게 늘리지 않음
妊娠アクメ堕ち2本立SP -> 임신 절정에 빠지는 섹스 2회 SP. 2편 구성 / 2본방으로 번역하지 않음
肛門イキ -> 애널 절정
2穴中出し集団痴●バス -> 2홀 질내사정 집단 치한 버스
痴●師 -> 치한. 치녀로 성별을 바꾸지 않음
辱める -> 성적 폭력 문맥에서 짓밟다 / 굴욕을 주다. 욕보이다는 사용하지 않음
嫌がる女を辱め力ずくの鬼畜姦 -> 싫어하는 여자들을 짓밟고 강제로 범하는 귀축 강간
囁き淫語 -> 속삭이는 음란어
極妻 -> 야쿠자 아내
アへ顔 -> 아헤가오. 생략하지 않음
ネットでAV応募→AV体験撮影 -> 인터넷으로 AV 지원→AV 체험 촬영. 양쪽 단계와 화살표를 보존
3P経験 -> 3P 경험. P를 누락하지 않음
4パコ2日 -> 2일간 섹스 4회
酔狂M -> 술에 미친 극M
飲酒でガチギマ -> 술에 완전히 취한. 약기운을 덧붙이지 않음
脳髄からアクメ心酔崩壊 -> 골수까지 절정에 취해 붕괴
激シコ -> 딸감 / 꼴리는. 생략하지 않음
タマころがし / タマ舐め -> 불알 굴리기 / 불알 핥기
脳汁 -> 성적 쾌감 문맥에서 쾌감. 뇌수로 직역하지 않음
3発射 -> 3회 사정
ダーツナンパ -> 다트 헌팅. 다츠 난파 / 다트 난파로 번역하지 않음
喉奥イマラ -> 목구멍 깊숙이 이라마치오
ハッスルSEX -> 화끈한 섹스
生チン / 生ちん / 生チ○ポ -> 자지. 생자지로 번역하지 않으며 生ハメ의 노콘과 구분
ズボズボ -> 깊숙이 쑤셔박기 / 깊숙이 박히는 피스톤. 즈보즈보로 음차하지 않음
際立たせる -> 돋보이게 하다
七変化 -> 일곱 번 변신. 수량을 누락하지 않음
発禁 -> 발매 금지
全身性感帯クリトリス -> 온몸이 성감대. 전신이 성감대 클리토리스처럼 신체를 합치지 않음
そこもっとしてして -> 거기 더 해줘
無理無理 -> 무리야, 무리야
ジュルジュル -> 자지를 빠는 문맥에서 쥬릅쥬릅 / 질척하게. 쥬릅쥬릅 자지를 빨아대다는 허용
なっち -> 활동명 / 별명일 때 낫치
みぃたん -> 활동명 / 별명일 때 미이짱
百合川さら / ゆりかわさら -> 유리카와 사라
久留木玲 / くるきれい -> 쿠루키 레이
三尾めぐ / みおめぐ -> 미오 메구
桜美ゆきな / さくらみゆきな -> 사쿠라미 유키나
ピュアで物静かなボブJ● -> 배우 필드에서는 이름이 아닌 묘사이므로 Unknown
激シコボディ -> 꼴리는 몸매 / 딸감 몸매
普通に責めるの上手い -> 애무 진짜 잘하네
シゴき上げフェラ -> 대딸하듯 훑어 올리는 펠라
美ボディー反り返りクンニ -> 허리가 휘도록 하는 보빨
円光 -> 조건만남
交縁界隈 -> 길거리 조건만남 판
立ちんぼ -> 길거리 성매매녀 / 길거리 성매매
ホ別 + 숫자 -> 호텔비 별도 + 숫자만 엔 (예: ホ別2 -> 호텔비 별도 2만 엔)
相場は1.5～ -> 시세는 1만 5천 엔부터
メン地下 -> 지하남돌
ガーシー -> 가십. 한국어 제목에서 가시로 음차하거나 원문에 없는 GaaSyy / Garsy 같은 라틴 별칭을 덧붙이지 않음
西麻布 -> 니시아자부. 니시아부 / 니시아부파로 번역하지 않음
デカ乳＆デカ尻のムワッ感 -> 거유와 큰 엉덩이의 짙은 색기
シュートを決める -> 성적 JAV 문맥: 한 발 쏘다 / 실제 스포츠 문맥: 슛을 성공시키다 또는 골을 넣다
Aにシュートを決める -> 성적 JAV 문맥: A에게 한 발 쏘다. A가 쏜 것으로 주객을 뒤집지 않음
華麗なシュートを決めてきました -> 성적 JAV 문맥: 제대로 한 발 쏘고 왔습니다
チームを勝利に導くマネージャーに華麗なシュートを決めてきました -> 팀을 승리로 이끄는 매니저에게 제대로 한 발 쏘고 왔습니다
シュート -> 실제 스포츠 문맥에서만 슛. 성적 중의성이 있으면 한 발 쏘다
小動物系 / 小動物系美少女 -> 소동물계 / 소동물계 미소녀. 의미를 작고 귀여운으로 풀어 쓰지 않음
利き手 -> 주로 쓰는 손 (좌우를 임의로 정하지 않음)
確変 -> 문맥에 맞게 돌변, 급격한 변화, 야함의 폭주로 의미 번역
げんえ./き -> げんえき -> 현역`

var defaultKoreanJAVDictionary = koreanJAVDictionaryFromEmbedded(fallbackKoreanJAVDictionary)

// defaultScraperPriority mirrors the priority order documented in
// configs/config.yaml.example so DefaultConfig(nil, nil) and the embedded
// example agree on scraper ordering (and thus on rating_source). Previously
// this started with "dmm" while the example started with "r18dev", causing
// getFirstScraperPriorityStatic() and DefaultConfig to set rating_source to
// "dmm" despite the example documenting "r18dev".
var defaultScraperPriority = []string{
	"r18dev", "fanzamcp", "libredmm", "dmm", "javlibrary",
	"javdb", "javbus", "jav321", "mgstage", "tokyohot", "aventertainment",
	"caribbeancom", "dlgetchu", "fc2", "paipancon", "123av", "javstash",
}

// getFirstScraperPriorityStatic returns the first element of the hardcoded
// default scraper priority list. Callers that need dynamic (registry-based)
// priorities should inject them via DefaultConfig() instead.
func getFirstScraperPriorityStatic() string {
	if len(defaultScraperPriority) > 0 {
		return defaultScraperPriority[0]
	}
	return ""
}

// defaultServerConfig returns the default server configuration.
func defaultServerConfig() ServerConfig {
	return ServerConfig{
		Host: "localhost",
		Port: 8765,
	}
}

// defaultAPIConfig returns the default API configuration.
func defaultAPIConfig() APIConfig {
	return APIConfig{
		Security: SecurityConfig{
			AllowedDirectories: []string{},
			DeniedDirectories:  []string{},
			MaxFilesPerScan:    0,
			ScanTimeoutSeconds: 0,
			AllowedOrigins: []string{
				"http://localhost:8765",
				"http://localhost:5173",
				"http://localhost:5174",
				"http://127.0.0.1:8765",
				"http://127.0.0.1:5173",
				"http://127.0.0.1:5174",
			},
			RateLimit: RateLimitConfig{
				RequestsPerMinute: 0,
			},
		},
	}
}

// defaultScraperConfig returns the default scraper configuration.
func defaultScraperConfig(priorities []string, defaults map[string]*models.ScraperSettings) ScrapersConfig {
	return ScrapersConfig{
		UserAgent:             "",
		Referer:               "https://www.dmm.co.jp/", // Referer header for CDN compatibility (required by DMM/R18 CDN)
		TimeoutSeconds:        30,                       // HTTP client timeout
		RequestTimeoutSeconds: 60,                       // Overall request timeout
		Priority:              priorities,               // Caller-injected scraper execution order
		FlareSolverr: models.FlareSolverrConfig{
			Enabled:    false,
			URL:        "http://localhost:8191/v1",
			Timeout:    30,
			MaxRetries: 3,
			SessionTTL: 300,
		},
		ScrapeActress:       true, // Global scrape_actress default (opt-out behavior)
		EarlyStop:           false,
		EarlyStopMinResults: 2,
		Browser: models.BrowserConfig{
			Enabled:      false, // Opt-in
			BinaryPath:   "",    // Auto-discovered if empty
			Timeout:      30,
			MaxRetries:   3,
			Headless:     true,
			StealthMode:  true,
			WindowWidth:  1920,
			WindowHeight: 1080,
			SlowMo:       0,
			BlockImages:  true,
			BlockCSS:     false,
			UserAgent:    "",
			DebugVisible: false,
		},
		Proxy: models.ProxyConfig{
			Enabled:        false,
			DefaultProfile: "main",
			Profiles: map[string]models.ProxyProfile{
				"main":   {URL: "", Username: "", Password: ""},
				"backup": {URL: "", Username: "", Password: ""},
			},
		},
		Overrides: defaults,
	}
}

// defaultTranslationConfig returns the default translation configuration.
func defaultTranslationConfig() TranslationConfig {
	thinkingDisabled := false
	return TranslationConfig{
		Enabled:                 false, // Opt-in to avoid API calls unless explicitly configured
		Provider:                translationProviderOpenAI,
		SourceLanguage:          "ja", // Japanese content translated to English
		TargetLanguage:          "en",
		TargetLanguages:         nil,
		TimeoutSeconds:          120,
		MaxConcurrency:          3,
		ApplyToPrimary:          true,
		OverwriteExistingTarget: true,
		DictionaryEnabled:       false,
		Dictionary:              defaultKoreanJAVDictionary,
		Fields: TranslationFieldsConfig{
			Title:         true,
			OriginalTitle: true,
			Description:   true,
			Director:      true,
			Maker:         true,
			Label:         true,
			Series:        true,
			Genres:        true,
			Actresses:     true,
		},
		OpenAI: OpenAITranslationConfig{
			BaseURL: "https://api.openai.com/v1",
			APIKey:  "",
			Model:   "gpt-4o-mini",
		},
		DeepL: DeepLTranslationConfig{
			Mode:    models.DeepLModeFree,
			BaseURL: "",
			APIKey:  "",
		},
		Google: GoogleTranslationConfig{
			Mode:    models.GoogleModeFree,
			BaseURL: "",
			APIKey:  "",
		},
		OpenAICompatible: OpenAICompatibleTranslationConfig{
			BaseURL:         "http://localhost:11434/v1",
			APIKey:          "",
			Model:           "",
			MaxOutputTokens: defaultOpenAICompatibleMaxOutputTokens,
			EnableThinking:  &thinkingDisabled,
			ThinkingMode:    "boolean",
		},
		Anthropic: AnthropicTranslationConfig{
			BaseURL: "https://api.anthropic.com",
			APIKey:  "",
			Model:   "claude-sonnet-4-20250514",
		},
		Bedrock: BedrockTranslationConfig{
			Region: "us-east-1",
			Model:  "anthropic.claude-3-5-sonnet-20241022-v2:0",
		},
	}
}

// defaultMetadataConfig returns the default metadata configuration.
func defaultMetadataConfig() MetadataConfig {
	return MetadataConfig{
		Priority: PriorityConfig{
			Priority: nil, // Derived from registered scraper priorities at runtime
		},
		ActressDatabase: ActressDatabaseConfig{
			Enabled:      true,
			AutoAdd:      true,
			ConvertAlias: false,
		},
		GenreReplacement: GenreReplacementConfig{
			Enabled: true,
			AutoAdd: true,
		},
		WordReplacement: WordReplacementConfig{
			Enabled: false, // Opt-in: rewrites all text fields
		},
		TagDatabase: tagDatabaseConfig{
			Enabled: false, // Opt-in feature for per-movie custom tags
		},
		R18DevDump: R18DevDumpConfig{
			Enabled: true,                         // Harmless without the dump file; activates on `javinizer dump download`
			Path:    "data/r18dev/r18dev_dump.db", // Relative to working dir, like the main DB
		},
		Translation:  defaultTranslationConfig(),
		IgnoreGenres: []string{},
		NFO:          defaultNFOConfig(),
		Completeness: defaultCompletenessConfig(),
	}
}

// defaultNFOConfig returns the default NFO configuration.
func defaultNFOConfig() NFOConfig {
	return NFOConfig{
		Feature: NFOFeatureConfig{
			Enabled:              true,
			PerFile:              false,
			IncludeFanart:        true,
			IncludeTrailer:       true,
			IncludeStreamDetails: false,
			IncludeOriginalPath:  false,
			ActressAsTag:         false,
			AddGenericRole:       false,
			AltNameRole:          false,
		},
		Format: NFOFormatConfig{
			DisplayTitle:       "<TITLE>",
			FilenameTemplate:   "<ID>.nfo",
			FirstNameOrder:     true, // NFO defaults to FirstName LastName (Kodi/Plex convention); output.first_name_order defaults to false (Japanese convention)
			ActressLanguageJA:  false,
			RatingSource:       getFirstScraperPriorityStatic(),
			Tagline:            "",
			UnknownActressMode: models.UnknownActressModeSkip,
			UnknownActressText: "Unknown",
		},
		Extra: NFOExtraConfig{},
	}
}

// defaultCompletenessConfig returns the default completeness configuration.
func defaultCompletenessConfig() completenessConfig {
	return completenessConfig{
		Enabled: false,
		Tiers: completenessTierConfig{
			Essential: completenessTierDefinition{
				Weight: 50,
				Fields: []string{"title", "poster_url", "cover_url", "actresses", "genres"},
			},
			Important: completenessTierDefinition{
				Weight: 35,
				Fields: []string{"description", "maker", "release_date", "director", "runtime", "trailer_url", "screenshot_urls"},
			},
			NiceToHave: completenessTierDefinition{
				Weight: 15,
				Fields: []string{"label", "series", "rating_score", "original_title", "translations"},
			},
		},
	}
}

// defaultOutputConfig returns the default output configuration.
func defaultOutputConfig() OutputConfig {
	return OutputConfig{
		Template: OutputTemplateConfig{
			FolderFormat:     "<ID> [<STUDIO>] - <TITLE> (<YEAR>)",
			FileFormat:       "<ID><IF:MULTIPART>-pt<PART></IF>",
			SubfolderFormat:  []string{"<ID>"},
			ActressDelimiter: ", ",
			MaxTitleLength:   100,
			MaxPathLength:    240,
			FirstNameOrder:   false, // Default to LastName FirstName (Japanese naming convention)
		},
		Operation: OutputOperationConfig{
			OperationMode:           "",
			RenameFile:              true,       // Rename files by default
			AllowRevert:             false,      // Opt-in: revert is disabled by default for safety
			GroupActress:            false,      // Don't group actresses by default
			GroupActressName:        "@Group",   // Default group name when group_actress is enabled
			GroupUnknownActressName: "@Unknown", // Default unknown-actress name when group_actress is enabled and the actress list is empty or unknown
			MoveSubtitles:           false,
			MoveFiles:               false,
			SubtitleExtensions:      []string{".srt", ".ass", ".ssa", ".smi", ".vtt"},
		},
		MediaFormat: OutputMediaFormatConfig{
			PosterFormat:      "<ID><IF:MULTIPART>-pt<PART></IF>-poster.jpg",
			MaxPosterHeight:   0, // No cap — preserve source resolution
			FanartFormat:      "<ID><IF:MULTIPART>-pt<PART></IF>-fanart.jpg",
			TrailerFormat:     "<ID>-trailer.mp4",
			ScreenshotFormat:  "fanart<INDEX>.jpg",
			ScreenshotFolder:  "extrafanart",
			ScreenshotPadding: 1,
			ActressFolder:     ".actors",
			ActressFormat:     "<ACTORNAME>.jpg",
		},
		Download: OutputDownloadConfig{
			DownloadCover:       true,
			DownloadPoster:      true,
			DownloadExtrafanart: true,
			DownloadTrailer:     true,
			DownloadActress:     true,
			DownloadTimeout:     60, // 60 seconds default
			DownloadProxy: models.ProxyConfig{
				Enabled: false,
			},
		},
	}
}

// defaultLoggingConfig returns the default logging configuration.
func defaultLoggingConfig() LoggingConfig {
	return LoggingConfig{
		Level:      "info",
		Format:     "text",
		Output:     "stdout,data/logs/javinizer.log",
		MaxSizeMB:  10,
		MaxBackups: 5,
		MaxAgeDays: 0,
		Compress:   true,
	}
}

// DefaultConfig returns the default configuration. Priorities and defaults are
// injected by the caller so the config package has no dependency on scraperutil.
// Pass nil for both to use the hardcoded fallbacks (suitable for structural
// defaults that YAML unmarshaling immediately overrides).
func DefaultConfig(priorities []string, defaults map[string]*models.ScraperSettings) *Config {
	// Use injected priorities or fall back to hardcoded defaults.
	if len(priorities) == 0 {
		priorities = defaultScraperPriority
	}
	// Use injected defaults or fall back to empty map.
	if defaults == nil {
		defaults = make(map[string]*models.ScraperSettings)
	}

	return &Config{
		ConfigVersion: CurrentConfigVersion,
		Server:        defaultServerConfig(),
		API:           defaultAPIConfig(),
		Scrapers:      defaultScraperConfig(priorities, defaults),
		Metadata:      defaultMetadataConfig(),
		Matching: MatchingConfig{
			Extensions:      []string{".mp4", ".mkv", ".avi", ".wmv", ".flv"},
			MinSizeMB:       0,
			ExcludePatterns: []string{"*-trailer*", "*-sample*"},
			RegexEnabled:    false,
			RegexPattern:    `([a-zA-Z|tT28]+-\d+[zZ]?[eE]?)(?:-pt)?(\d{1,2})?`,
		},
		Output: defaultOutputConfig(),
		Database: DatabaseConfig{
			Type:     "sqlite",
			DSN:      "data/javinizer.db",
			LogLevel: "silent", // Default: no SQL query logging
		},
		Logging: defaultLoggingConfig(),
		Performance: PerformanceConfig{
			MaxWorkers:     5,
			WorkerTimeout:  300,
			BufferSize:     100,
			UpdateInterval: 100,
		},
		MediaInfo: mediaInfoConfig{
			CLIEnabled: false,
			CLIPath:    "mediainfo",
			CLITimeout: 30,
		},
		System: SystemConfig{
			Umask:                     "002",
			VersionCheckEnabled:       true,
			VersionCheckIntervalHours: 24,
			TempDir:                   DefaultTempDir,
		},
		// VersionCheckStableOnly is intentionally omitted: its zero value (false)
		// is the correct default (prereleases allowed). Existing configs that
		// lack the field inherit false via decodeConfig's load-into-DefaultConfig,
		// so no migration is required.
		WebUI: webUIConfig{
			DefaultReviewView: "grid-poster",
			Favorites:         FavoritesConfig{Genre: []string{}},
		},
	}
}
