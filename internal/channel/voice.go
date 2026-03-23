package channel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"sync/atomic"
	"time"
)

// AudioFormat specifies the audio encoding.
type AudioFormat string

const (
	AudioWav  AudioFormat = "wav"
	AudioOpus AudioFormat = "opus"
	AudioMP3  AudioFormat = "mp3"
	AudioOgg  AudioFormat = "ogg"
	AudioPCM  AudioFormat = "pcm"
)

// VoiceConfig holds voice adapter configuration.
type VoiceConfig struct {
	STTModel   string `mapstructure:"stt_model"`    // default whisper-large-v3
	TTSModel   string `mapstructure:"tts_model"`    // default tts-1
	TTSVoice   string `mapstructure:"tts_voice"`    // default alloy
	LocalSTT   bool   `mapstructure:"local_stt"`
	LocalTTS   bool   `mapstructure:"local_tts"`
	SampleRate int    `mapstructure:"sample_rate"`   // default 16000
	APIKey     string `mapstructure:"api_key"`
	APIBaseURL string `mapstructure:"api_base_url"`  // default OpenAI
}

// VoiceAdapter handles speech-to-text and text-to-speech.
// Not a full Adapter (no Recv/Send cycle) — used as a service by other adapters.
type VoiceAdapter struct {
	cfg              VoiceConfig
	client           *http.Client
	transcribeCount  atomic.Int64
	synthesizeCount  atomic.Int64
}

// TranscriptionResult holds the output of speech-to-text.
type TranscriptionResult struct {
	Text       string  `json:"text"`
	Language   string  `json:"language,omitempty"`
	Duration   float64 `json:"duration,omitempty"`
	Confidence float64 `json:"confidence"`
}

// NewVoiceAdapter creates a voice adapter.
func NewVoiceAdapter(cfg VoiceConfig) *VoiceAdapter {
	if cfg.STTModel == "" {
		cfg.STTModel = "whisper-large-v3"
	}
	if cfg.TTSModel == "" {
		cfg.TTSModel = "tts-1"
	}
	if cfg.TTSVoice == "" {
		cfg.TTSVoice = "alloy"
	}
	if cfg.SampleRate == 0 {
		cfg.SampleRate = 16000
	}
	if cfg.APIBaseURL == "" {
		cfg.APIBaseURL = "https://api.openai.com/v1"
	}
	return &VoiceAdapter{
		cfg: cfg,
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

func (v *VoiceAdapter) PlatformName() string { return "voice" }

// Transcribe converts audio to text using the STT API.
func (v *VoiceAdapter) Transcribe(ctx context.Context, audio []byte, format AudioFormat) (*TranscriptionResult, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	part, err := writer.CreateFormFile("file", "audio."+string(format))
	if err != nil {
		return nil, fmt.Errorf("voice transcribe form: %w", err)
	}
	if _, err := part.Write(audio); err != nil {
		return nil, err
	}
	writer.WriteField("model", v.cfg.STTModel)
	writer.WriteField("response_format", "verbose_json")
	writer.Close()

	req, err := http.NewRequestWithContext(ctx, "POST", v.cfg.APIBaseURL+"/audio/transcriptions", &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if v.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+v.cfg.APIKey)
	}

	resp, err := v.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("voice transcribe: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("voice transcribe %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Text     string  `json:"text"`
		Language string  `json:"language"`
		Duration float64 `json:"duration"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("voice transcribe decode: %w", err)
	}

	v.transcribeCount.Add(1)

	return &TranscriptionResult{
		Text:       result.Text,
		Language:   result.Language,
		Duration:   result.Duration,
		Confidence: 1.0,
	}, nil
}

// Synthesize converts text to audio using the TTS API.
func (v *VoiceAdapter) Synthesize(ctx context.Context, text string) ([]byte, error) {
	payload := map[string]string{
		"model": v.cfg.TTSModel,
		"voice": v.cfg.TTSVoice,
		"input": text,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST", v.cfg.APIBaseURL+"/audio/speech", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if v.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+v.cfg.APIKey)
	}

	resp, err := v.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("voice synthesize: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("voice synthesize %d: %s", resp.StatusCode, string(respBody))
	}

	audio, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("voice synthesize read: %w", err)
	}

	v.synthesizeCount.Add(1)
	return audio, nil
}

// Stats returns transcription and synthesis counters.
func (v *VoiceAdapter) Stats() (transcriptions, syntheses int64) {
	return v.transcribeCount.Load(), v.synthesizeCount.Load()
}
