/**
 * MessageInput Component
 * Input field for user messages
 */
import React, { useState, useRef } from 'react';
import { useTranslation } from 'react-i18next';

const MessageInput = ({ onSend, onStop, disabled, isGenerating }) => {
  const { t } = useTranslation();
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
          placeholder={t('messageInput.placeholder')}
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
            ⬛ {t('messageInput.stop')}
          </button>
        ) : (
          <button 
            type="submit" 
            disabled={disabled || !input.trim()}
            className="send-button"
          >
            {t('messageInput.send')}
          </button>
        )}
      </div>
    </form>
  );
};

export default MessageInput;
