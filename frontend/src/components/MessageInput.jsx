/**
 * MessageInput Component
 * Input field for user messages
 */
import React, { useState, useRef } from 'react';

const MessageInput = ({ onSend, onStop, disabled, isGenerating }) => {
  const [input, setInput] = useState('');
  const textareaRef = useRef(null);

  const handleSubmit = (e) => {
    e.preventDefault();
    if (!disabled && input.trim()) {
      onSend(input.trim());
      setInput('');
    }
  };

  const handleStop = (e) => {
    e.preventDefault();
    onStop && onStop();
  };

  const handleKeyPress = (e) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      if (!isGenerating) {
        handleSubmit(e);
      }
    }
  };

  return (
    <form onSubmit={handleSubmit} className="message-input-form">
      <div className="input-row">
        <textarea
          ref={textareaRef}
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyPress={handleKeyPress}
          placeholder="请输入您的问题..."
          disabled={disabled}
          rows="3"
          className="message-input"
        />
        {isGenerating ? (
          <button 
            type="button"
            onClick={handleStop}
            className="stop-button"
          >
            ⬛ 停止
          </button>
        ) : (
          <button 
            type="submit" 
            disabled={disabled || !input.trim()}
            className="send-button"
          >
            发送
          </button>
        )}
      </div>
    </form>
  );
};

export default MessageInput;
