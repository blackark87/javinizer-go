package config

import "github.com/javinizer/javinizer-go/internal/models"

const defaultKoreanJAVDictionary = `JAV 문체 -> 성기·성행위·사정 표현은 원문의 노골성을 유지해 보지, 자지, 보빨, 대딸, 박다, 싸다처럼 직설적으로 번역. 소중이, 그곳, 중요 부위 같은 완곡어로 순화하지 않음
문맥 원칙 -> 비성적 일반어를 억지로 음란하게 만들지 않되, 성적 중의어는 JAV 문맥을 우선
中出し -> 질내사정(제목·장르) / 안에 싸다(거친 대사·서술)
手コキ / ハンドジョブ / handjob -> 대딸
フェラ / フェラチオ -> 펠라(제목·장르) / 자지 빨기(거친 행위 묘사)
クンニ -> 보빨
パイパン -> 백보지
美脚 -> 각선미
美乳 -> 예쁜 가슴
巨乳 -> 거유
爆乳 -> 폭유
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
ドM -> 극M
即尺 -> 바로 빨기
即ハメ -> 바로 박기
手マン -> 핑거링(제목·장르) / 보지를 손가락으로 쑤시다(거친 서술)
ガシガシ手マン -> 거친 핑거링 / 보지를 손가락으로 거칠게 쑤시다
激ピス -> 격렬한 피스톤
電クリ -> 전동 클리 자극
鬼イカセ -> 무자비 강제 절정. 연속 표현은 원문에 연속성이 있을 때만 사용
ガン突き -> 쑤셔박기
激イキ -> 격렬 절정
大放出 -> 분수 대방출 / 정액 대방출. 방출 대상을 원문 문맥에서 판단
ガン突き激イキ大放出セックス -> 쑤셔박기·격렬 절정·분수 대방출 섹스
ハメる / ハメられる -> 박다 / 박히다
挿れる -> 박아 넣다
射精 -> 사정 / 싸다
ザーメン / 精液 -> 정액
ケツマンコ -> 후장
アナル -> 애널
性獣 -> 색마
ヤリモク -> 섹스만 노리는
股コキ -> 가랑이딸
太ももコキ -> 허벅지딸
尻コキ -> 엉덩이딸
おっパブ -> 옵파이 펍 또는 슴가 펍
ハメ撮り -> POV 섹스 / 셀프 섹스 촬영. 일반 셀프카메라로 순화하지 않음
秘蔵 -> 미공개 또는 비공개 소장 (영상 홍보 문맥)
蔵出し / 蔵出し動画 -> 미공개 영상 또는 소장 영상 공개
ガルバ -> 걸즈바
気弱 -> 소심한
シュートを決める -> 성적 JAV 문맥: 한 발 쏘다 / 실제 스포츠 문맥: 슛을 성공시키다 또는 골을 넣다
Aにシュートを決める -> 성적 JAV 문맥: A에게 한 발 쏘다. A가 쏜 것으로 주객을 뒤집지 않음
華麗なシュートを決めてきました -> 성적 JAV 문맥: 제대로 한 발 쏘고 왔습니다
チームを勝利に導くマネージャーに華麗なシュートを決めてきました -> 팀을 승리로 이끄는 매니저에게 제대로 한 발 쏘고 왔습니다
シュート -> 실제 스포츠 문맥에서만 슛. 성적 중의성이 있으면 한 발 쏘다
小動物系 / 小動物系美少女 -> 소동물계 / 소동물계 미소녀. 의미를 작고 귀여운으로 풀어 쓰지 않음
利き手 -> 주로 쓰는 손 (좌우를 임의로 정하지 않음)
確変 -> 문맥에 맞게 돌변, 급격한 변화, 야함의 폭주로 의미 번역
げんえ./き -> げんえき -> 현역`

// defaultScraperPriority mirrors the priority order documented in
// configs/config.yaml.example so DefaultConfig(nil, nil) and the embedded
// example agree on scraper ordering (and thus on rating_source). Previously
// this started with "dmm" while the example started with "r18dev", causing
// getFirstScraperPriorityStatic() and DefaultConfig to set rating_source to
// "dmm" despite the example documenting "r18dev".
var defaultScraperPriority = []string{
	"r18dev", "libredmm", "dmm", "javlibrary",
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
