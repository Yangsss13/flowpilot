package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Database   DatabaseConfig
	Redis      RedisConfig
	RabbitMQ   RabbitMQConfig
	Qdrant     QdrantConfig
	AI         AIConfig
	Checkpoint CheckpointConfig
	Knowledge  KnowledgeConfig
	Server     ServerConfig
}

type KnowledgeConfig struct {
	StorageDir           string
	MaxBytesByFormat     map[string]int64
	MaxArchiveFiles      int
	MaxArchiveBytes      int64
	MaxArchiveRatio      int
	MaxArchiveDepth      int
	MaxPDFPages          int
	MaxPPTSlides         int
	ParseTimeout         time.Duration
	ChunkMaxRunes        int
	WorkerCount          int
	MaxRetries           int
	DispatchInterval     time.Duration
	SearchMinScore       float64
	FFmpegPath           string
	FFprobePath          string
	WhisperPath          string
	WhisperModelPath     string
	WhisperModelMinBytes int64
	WhisperLanguage      string
	TesseractPath        string
	TesseractDataDir     string
	OCRLanguages         string
	OCRMinConfidence     int
	MaxMediaDuration     time.Duration
	MaxVideoWidth        int
	MaxVideoHeight       int
	MediaJobTimeout      time.Duration
	MediaTempDir         string
	ProbeTimeout         time.Duration
	FFmpegTimeout        time.Duration
	ASRTimeout           time.Duration
	OCRTimeout           time.Duration
	FFmpegThreads        int
	WhisperThreads       int
	MediaConcurrency     int
	ASRConcurrency       int
	OCRConcurrency       int
	KeyframeInterval     time.Duration
	MaxKeyframes         int
}

type CheckpointConfig struct {
	Dir string
}

type ServerConfig struct {
	Port string
}

type DatabaseConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	Name     string
}

type RedisConfig struct {
	Addr string
}

type RabbitMQConfig struct {
	URL string
}

type QdrantConfig struct {
	URL        string
	Collection string
	APIKey     string
}

type AIConfig struct {
	BaseURL          string
	APIKey           string
	ChatModel        string
	EmbeddingModel   string
	HTTPAllowedHosts []string
}

func Load() Config {
	return Config{
		Database: DatabaseConfig{
			Host:     envOrDefault("DB_HOST", "127.0.0.1"),
			Port:     envOrDefault("DB_PORT", "3306"),
			User:     envOrDefault("DB_USER", "minikvx"),
			Password: os.Getenv("DB_PASSWORD"),
			Name:     envOrDefault("DB_NAME", "minikvx_agent"),
		},
		Redis: RedisConfig{
			Addr: envOrDefault("REDIS_ADDR", "127.0.0.1:6379"),
		},
		RabbitMQ: RabbitMQConfig{
			URL: envOrDefault("RABBITMQ_URL", "amqp://guest:guest@127.0.0.1:5672/"),
		},
		Qdrant: QdrantConfig{
			URL:        envOrDefault("QDRANT_URL", "http://127.0.0.1:6334"),
			Collection: envOrDefault("QDRANT_COLLECTION", "flowpilot_knowledge"),
			APIKey:     os.Getenv("QDRANT_API_KEY"),
		},
		AI: AIConfig{
			BaseURL:          envOrDefault("AI_BASE_URL", "https://api.siliconflow.cn/v1"),
			APIKey:           os.Getenv("AI_API_KEY"),
			ChatModel:        os.Getenv("AI_CHAT_MODEL"),
			EmbeddingModel:   os.Getenv("AI_EMBEDDING_MODEL"),
			HTTPAllowedHosts: splitList(os.Getenv("HTTP_TOOL_ALLOWED_HOSTS")),
		},
		Checkpoint: CheckpointConfig{
			Dir: envOrDefault("CHECKPOINT_DIR", "./data/checkpoints"),
		},
		Knowledge: KnowledgeConfig{
			StorageDir: envOrDefault("KNOWLEDGE_STORAGE_DIR", "./data/knowledge/objects"),
			MaxBytesByFormat: map[string]int64{
				".txt":  envInt64("KNOWLEDGE_MAX_TXT_BYTES", 5<<20),
				".md":   envInt64("KNOWLEDGE_MAX_MD_BYTES", 5<<20),
				".pdf":  envInt64("KNOWLEDGE_MAX_PDF_BYTES", 25<<20),
				".docx": envInt64("KNOWLEDGE_MAX_DOCX_BYTES", 50<<20),
				".pptx": envInt64("KNOWLEDGE_MAX_PPTX_BYTES", 50<<20),
				".mp3":  envInt64("KNOWLEDGE_MAX_MP3_BYTES", 100<<20),
				".wav":  envInt64("KNOWLEDGE_MAX_WAV_BYTES", 100<<20),
				".m4a":  envInt64("KNOWLEDGE_MAX_M4A_BYTES", 100<<20),
				".mp4":  envInt64("KNOWLEDGE_MAX_MP4_BYTES", 500<<20),
				".mov":  envInt64("KNOWLEDGE_MAX_MOV_BYTES", 500<<20),
				".webm": envInt64("KNOWLEDGE_MAX_WEBM_BYTES", 500<<20),
			},
			MaxArchiveFiles:      envInt("KNOWLEDGE_MAX_ARCHIVE_FILES", 2000),
			MaxArchiveBytes:      envInt64("KNOWLEDGE_MAX_ARCHIVE_BYTES", 200<<20),
			MaxArchiveRatio:      envInt("KNOWLEDGE_MAX_ARCHIVE_RATIO", 100),
			MaxArchiveDepth:      envInt("KNOWLEDGE_MAX_ARCHIVE_DEPTH", 10),
			MaxPDFPages:          envInt("KNOWLEDGE_MAX_PDF_PAGES", 500),
			MaxPPTSlides:         envInt("KNOWLEDGE_MAX_PPT_SLIDES", 500),
			ParseTimeout:         envDuration("KNOWLEDGE_PARSE_TIMEOUT", 45*time.Second),
			ChunkMaxRunes:        envInt("KNOWLEDGE_CHUNK_MAX_RUNES", 1200),
			WorkerCount:          envInt("KNOWLEDGE_WORKER_COUNT", 2),
			MaxRetries:           envInt("KNOWLEDGE_MAX_RETRIES", 3),
			DispatchInterval:     envDuration("KNOWLEDGE_DISPATCH_INTERVAL", 10*time.Second),
			SearchMinScore:       envFloat("KNOWLEDGE_SEARCH_MIN_SCORE", 0.5),
			FFmpegPath:           envOrDefault("FFMPEG_PATH", "ffmpeg"),
			FFprobePath:          envOrDefault("FFPROBE_PATH", "ffprobe"),
			WhisperPath:          envOrDefault("WHISPER_CPP_PATH", "whisper-cli"),
			WhisperModelPath:     os.Getenv("WHISPER_MODEL_PATH"),
			WhisperModelMinBytes: envInt64("WHISPER_MODEL_MIN_BYTES", 400<<20),
			WhisperLanguage:      envOrDefault("WHISPER_LANGUAGE", "auto"),
			TesseractPath:        envOrDefault("TESSERACT_PATH", "tesseract"),
			TesseractDataDir:     envOrDefault("TESSERACT_DATA_DIR", "./data/tools/tessdata-fast"),
			OCRLanguages:         envOrDefault("OCR_LANGUAGES", "chi_sim+eng"),
			OCRMinConfidence:     envInt("OCR_MIN_CONFIDENCE", 60),
			MaxMediaDuration:     envDuration("KNOWLEDGE_MAX_MEDIA_DURATION", 2*time.Hour),
			MaxVideoWidth:        envInt("KNOWLEDGE_MAX_VIDEO_WIDTH", 3840),
			MaxVideoHeight:       envInt("KNOWLEDGE_MAX_VIDEO_HEIGHT", 2160),
			MediaJobTimeout:      envDuration("KNOWLEDGE_MEDIA_JOB_TIMEOUT", 45*time.Minute),
			MediaTempDir:         envOrDefault("KNOWLEDGE_MEDIA_TEMP_DIR", os.TempDir()),
			ProbeTimeout:         envDuration("KNOWLEDGE_PROBE_TIMEOUT", 15*time.Second),
			FFmpegTimeout:        envDuration("KNOWLEDGE_FFMPEG_TIMEOUT", 10*time.Minute),
			ASRTimeout:           envDuration("KNOWLEDGE_ASR_TIMEOUT", 30*time.Minute),
			OCRTimeout:           envDuration("KNOWLEDGE_OCR_TIMEOUT", 20*time.Second),
			FFmpegThreads:        envInt("KNOWLEDGE_FFMPEG_THREADS", 2),
			WhisperThreads:       envInt("KNOWLEDGE_WHISPER_THREADS", 4),
			MediaConcurrency:     envInt("KNOWLEDGE_MEDIA_CONCURRENCY", 1),
			ASRConcurrency:       envInt("KNOWLEDGE_ASR_CONCURRENCY", 1),
			OCRConcurrency:       envInt("KNOWLEDGE_OCR_CONCURRENCY", 2),
			KeyframeInterval:     envDuration("KNOWLEDGE_KEYFRAME_INTERVAL", 20*time.Second),
			MaxKeyframes:         envInt("KNOWLEDGE_MAX_KEYFRAMES", 120),
		},
		Server: ServerConfig{
			Port: envOrDefault("APP_PORT", "8080"),
		},
	}
}

func envInt(key string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(key))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func envInt64(key string, fallback int64) int64 {
	value, err := strconv.ParseInt(os.Getenv(key), 10, 64)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func envFloat(key string, fallback float64) float64 {
	value, err := strconv.ParseFloat(os.Getenv(key), 64)
	if err != nil || value < 0 || value > 1 {
		return fallback
	}
	return value
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value, err := time.ParseDuration(os.Getenv(key))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func splitList(value string) []string {
	var values []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			values = append(values, item)
		}
	}
	return values
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
