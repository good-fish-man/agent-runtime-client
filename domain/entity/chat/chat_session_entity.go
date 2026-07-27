package chat

// ChatSession is a chat session.
type ChatSession struct {
	Ulid      string `json:"ulid"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
	DeletedAt int64  `json:"deleted_at"`
	UserId    string `json:"user_id"`
	AgentId   string `json:"agent_id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	Channel   string `json:"channel"`
}

func (c *ChatSession) IsActive() bool   { return c.Status == "active" }
func (c *ChatSession) IsArchived() bool { return c.Status == "archived" }
func (c *ChatSession) IsDeleted() bool  { return c.DeletedAt > 0 }
