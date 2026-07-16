package models

import (
	"database/sql/driver"
	"time"
)

// ---------------------------------------------------------------------------
// Typed enums — type X string for GORM compatibility with existing string columns
// ---------------------------------------------------------------------------

// EventCategory represents the category of a structured event log entry.
type EventCategory string

// EventCategory values classifying the source of a structured event log entry.
const (
	EventCategoryScraper  EventCategory = "scraper"
	EventCategoryOrganize EventCategory = "organize"
	EventCategorySystem   EventCategory = "system"
)

func (e EventCategory) String() string { return string(e) }

// MarshalJSON implements json.Marshaler for EventCategory.
func (e EventCategory) MarshalJSON() ([]byte, error) { return MarshalStringEnum(string(e)) }

// UnmarshalJSON implements json.Unmarshaler for EventCategory.
func (e *EventCategory) UnmarshalJSON(b []byte) error { return UnmarshalStringEnum((*string)(e), b) }

// Scan implements sql.Scanner for EventCategory.
func (e *EventCategory) Scan(value any) error { return ScanStringEnum((*string)(e), value) }

// Value implements driver.Valuer for EventCategory.
func (e EventCategory) Value() (driver.Value, error) { return StringEnumValue(string(e)) }

// EventSeverity represents the severity level of a structured event log entry.
type EventSeverity string

// EventSeverity values ranking the importance of a structured event log entry.
const (
	SeverityDebug EventSeverity = "debug"
	SeverityInfo  EventSeverity = "info"
	SeverityWarn  EventSeverity = "warn"
	SeverityError EventSeverity = "error"
)

func (e EventSeverity) String() string { return string(e) }

// MarshalJSON implements json.Marshaler for EventSeverity.
func (e EventSeverity) MarshalJSON() ([]byte, error) { return MarshalStringEnum(string(e)) }

// UnmarshalJSON implements json.Unmarshaler for EventSeverity.
func (e *EventSeverity) UnmarshalJSON(b []byte) error { return UnmarshalStringEnum((*string)(e), b) }

// Scan implements sql.Scanner for EventSeverity.
func (e *EventSeverity) Scan(value any) error { return ScanStringEnum((*string)(e), value) }

// Value implements driver.Valuer for EventSeverity.
func (e EventSeverity) Value() (driver.Value, error) { return StringEnumValue(string(e)) }

// Event represents a structured event log entry for debugging and bug reporting
type Event struct {
	ID        uint          `json:"id" gorm:"primaryKey"`
	EventType EventCategory `json:"event_type" gorm:"not null;index:idx_events_type;index:idx_events_type_severity;index:idx_events_type_source"`
	Severity  EventSeverity `json:"severity" gorm:"not null;index:idx_events_severity;index:idx_events_type_severity"`
	Message   string        `json:"message" gorm:"not null;type:text"`
	Context   string        `json:"context" gorm:"type:text"` // JSON-encoded details
	Source    string        `json:"source" gorm:"index:idx_events_source;index:idx_events_type_source"`
	CreatedAt time.Time     `json:"created_at" gorm:"not null;index:idx_events_created_at"`
}

// TableName specifies the table name for Event
func (Event) TableName() string {
	return "events"
}
