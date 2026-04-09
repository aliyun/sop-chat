/**
 * Message Component
 * Displays message with events (tool calls and content) in true chronological order
 */
import React, { useState } from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { submitFeedback } from '../services/api';
import { copyToClipboard } from '../utils/clipboard';
import { useTranslation } from 'react-i18next';

// Custom link renderer for ReactMarkdown - open links in new tab
const LinkRenderer = ({ href, children }) => {
  return (
    <a href={href} target="_blank" rel="noopener noreferrer">
      {children}
    </a>
  );
};

const Message = ({ 
  role, 
  content, 
  isStreaming = false, 
  events = [], 
  stage,
  conversationId = null,  // For feedback
  requestId = null,       // For feedback
  question = null,        // Original question for feedback context
  hasImage = false,       // Deprecated: for backward compatibility
  imageData = null,       // Deprecated: single image (backward compatibility)
  hasImages = false,      // Indicates if user sent image(s)
  imageCount = 0,         // Number of images
  imageDatas = null,      // Array of Base64 image data URLs
  isShared = false,       // Is this a shared conversation (read-only)
  assistantName = null    // Assistant display name (from employee config)
}) => {
  const { t } = useTranslation();
  const isUser = role === 'user';
  const [expandedThinking, setExpandedThinking] = useState({});
  const [expandedTools, setExpandedTools] = useState({});
  const [feedbackStatus, setFeedbackStatus] = useState(null); // null, 'like', 'dislike'
  const [showFeedbackModal, setShowFeedbackModal] = useState(false);
  const [feedbackReason, setFeedbackReason] = useState('');
  const [isSubmittingFeedback, setIsSubmittingFeedback] = useState(false);
  const [showImagePreview, setShowImagePreview] = useState(false); // For image preview modal
  const [previewImageIndex, setPreviewImageIndex] = useState(0); // Index of image being previewed
  const [copySuccess, setCopySuccess] = useState(false); // Copy success indicator
  
  // Support both old single image and new multiple images format
  const images = imageDatas || (imageData ? [imageData] : []);
  
  // Toggle thinking block expansion
  const toggleThinking = (id) => {
    setExpandedThinking(prev => ({
      ...prev,
      [id]: !prev[id]
    }));
  };
  
  // Toggle tool call expansion
  const toggleTool = (id) => {
    setExpandedTools(prev => ({
      ...prev,
      [id]: !prev[id]
    }));
  };
  
  // Get the full answer text from events (excluding <think> blocks)
  const getAnswerText = () => {
    if (content) return content;
    if (!events || events.length === 0) return '';
    
    // Get all content chunks
    const fullContent = events
      .filter(e => e.type === 'content')
      .map(e => e.data)
      .join('');
    
    // Remove <think></think> blocks from the content
    return fullContent.replace(/<think>[\s\S]*?<\/think>/g, '').trim();
  };
  
  // Handle copy button click
  const handleCopy = async () => {
    const textToCopy = getAnswerText();
    if (!textToCopy) return;
    
    try {
      const ok = await copyToClipboard(textToCopy);
      if (!ok) {
        throw new Error('copy_failed');
      }
      setCopySuccess(true);
      
      // Submit copy action as feedback (only if not shared and has conversation info)
      if (!isShared && conversationId && requestId) {
        try {
          await submitFeedback(
            conversationId,
            requestId,
            'copy',
            null,
            question,
            textToCopy
          );
        } catch (feedbackError) {
          // Don't block copy operation if feedback fails
        }
      }
      
      // Reset success indicator after 2 seconds
      setTimeout(() => {
        setCopySuccess(false);
      }, 2000);
    } catch (error) {
      console.error('Failed to copy text:', error);
    }
  };
  
  // Handle like button click
  const handleLike = async () => {
    if (feedbackStatus || !conversationId || !requestId) return;
    
    setIsSubmittingFeedback(true);
    try {
      await submitFeedback(
        conversationId,
        requestId,
        'like',
        null,
        question,
        getAnswerText()
      );
      setFeedbackStatus('like');
    } catch (error) {
      console.error('Failed to submit like:', error);
    } finally {
      setIsSubmittingFeedback(false);
    }
  };
  
  // Handle dislike button click - show modal
  const handleDislike = () => {
    if (feedbackStatus) return;
    setShowFeedbackModal(true);
  };
  
  // Handle dislike submit with reason
  const handleDislikeSubmit = async () => {
    if (!conversationId || !requestId) return;
    
    setIsSubmittingFeedback(true);
    try {
      await submitFeedback(
        conversationId,
        requestId,
        'dislike',
        feedbackReason || null,
        question,
        getAnswerText()
      );
      setFeedbackStatus('dislike');
      setShowFeedbackModal(false);
      setFeedbackReason('');
    } catch (error) {
      console.error('Failed to submit dislike:', error);
    } finally {
      setIsSubmittingFeedback(false);
    }
  };
  
  // Close feedback modal
  const handleCloseModal = () => {
    setShowFeedbackModal(false);
    setFeedbackReason('');
  };
  
  // Open image preview
  const handleImageClick = (index = 0) => {
    setPreviewImageIndex(index);
    setShowImagePreview(true);
  };
  
  // Close image preview
  const handleCloseImagePreview = () => {
    setShowImagePreview(false);
  };
  
  // Navigate to previous image in preview
  const handlePrevImage = (e) => {
    e.stopPropagation();
    setPreviewImageIndex((prev) => (prev > 0 ? prev - 1 : images.length - 1));
  };
  
  // Navigate to next image in preview
  const handleNextImage = (e) => {
    e.stopPropagation();
    setPreviewImageIndex((prev) => (prev < images.length - 1 ? prev + 1 : 0));
  };
  
  // Parse content to extract <think></think> blocks
  const parseThinkingBlocks = (text) => {
    if (!text) return [{ type: 'text', content: text }];
    
    const parts = [];
    let lastIndex = 0;
    const thinkRegex = /<think>([\s\S]*?)<\/think>/g;
    let match;
    let thinkingIndex = 0;
    
    while ((match = thinkRegex.exec(text)) !== null) {
      // Add text before the thinking block
      if (match.index > lastIndex) {
        const beforeText = text.slice(lastIndex, match.index);
        if (beforeText.trim()) {
          parts.push({ type: 'text', content: beforeText });
        }
      }
      
      // Add thinking block
      parts.push({
        type: 'thinking',
        content: match[1].trim(),
        id: thinkingIndex++
      });
      
      lastIndex = match.index + match[0].length;
    }
    
    // Add remaining text after last thinking block
    if (lastIndex < text.length) {
      const afterText = text.slice(lastIndex);
      if (afterText.trim()) {
        parts.push({ type: 'text', content: afterText });
      }
    }
    
    return parts.length > 0 ? parts : [{ type: 'text', content: text }];
  };
  
  // Process events: merge consecutive content chunks and filter out superseded tool call phases
  const processEvents = (rawEvents) => {
    if (!rawEvents || rawEvents.length === 0) {
      // Fallback: if no events but have content, create a single content event
      if (content) {
        return [{
          type: 'content',
          data: content,
          id: 'content-0'
        }];
      }
      return [];
    }
    
    const processed = [];
    let contentBuffer = [];
    
    // First pass: identify tool calls that have been updated from 'start' to 'success'/'fail'
    // We want to skip 'start' events if a later event for the same tool has a final state
    // BUT we need to preserve args from start event if final event has empty args
    const toolCallIndices = new Map(); // toolName -> [indices of events for this tool]
    rawEvents.forEach((event, idx) => {
      if (event.type === 'tool_call') {
        const toolName = event.data.tool;
        if (!toolCallIndices.has(toolName)) {
          toolCallIndices.set(toolName, []);
        }
        toolCallIndices.get(toolName).push(idx);
      }
    });
    
    // For each tool, determine which indices to skip and merge args if needed
    const indicesToSkip = new Set();
    const eventReplacements = new Map(); // idx -> modified event data
    
    for (const [toolName, indices] of toolCallIndices.entries()) {
      // If there are multiple events for the same tool, merge them
      if (indices.length > 1) {
        // Check if the last event has a final phase (success/fail)
        const lastIdx = indices[indices.length - 1];
        const lastEvent = rawEvents[lastIdx];
        
        if (lastEvent.data.phase === 'success' || lastEvent.data.phase === 'fail') {
          // Merge args from all earlier events into the last one
          let mergedArgs = { ...lastEvent.data.args };
          
          // If last event has empty args, try to get args from earlier events
          const lastEventHasArgs = mergedArgs && typeof mergedArgs === 'object' && 
                                    Object.keys(mergedArgs).length > 0;
          
          if (!lastEventHasArgs) {
            // Look for args in earlier events (usually the 'start' event)
            for (let i = 0; i < indices.length - 1; i++) {
              const earlierEvent = rawEvents[indices[i]];
              if (earlierEvent.data.args && typeof earlierEvent.data.args === 'object' && 
                  Object.keys(earlierEvent.data.args).length > 0) {
                mergedArgs = { ...earlierEvent.data.args, ...mergedArgs };
                break; // Use the first non-empty args found
              }
            }
          }
          
          // Create a modified version of the last event with merged args
          if (Object.keys(mergedArgs).length > 0) {
            eventReplacements.set(lastIdx, {
              ...lastEvent.data,
              args: mergedArgs
            });
          }
          
          // Skip all earlier events for this tool
          for (let i = 0; i < indices.length - 1; i++) {
            indicesToSkip.add(indices[i]);
          }
        }
      }
    }
    
    rawEvents.forEach((event, idx) => {
      // Skip events marked for removal
      if (indicesToSkip.has(idx)) {
        return;
      }
      
      if (event.type === 'content') {
        // Buffer content chunks
        contentBuffer.push(event.data);
      } else if (event.type === 'tool_call') {
        // Before adding tool call, flush content buffer
        if (contentBuffer.length > 0) {
          processed.push({
            type: 'content',
            data: contentBuffer.join(''),
            id: `content-${processed.length}`
          });
          contentBuffer = [];
        }
        
        // Use modified event data if available, otherwise use original
        const eventData = eventReplacements.has(idx) ? eventReplacements.get(idx) : event.data;
        
        // Add tool call
        processed.push({
          type: 'tool_call',
          data: eventData,
          id: `tool-${processed.length}`
        });
      } else if (event.type === 'error') {
        // Before adding error, flush content buffer
        if (contentBuffer.length > 0) {
          processed.push({
            type: 'content',
            data: contentBuffer.join(''),
            id: `content-${processed.length}`
          });
          contentBuffer = [];
        }
        // Add error event
        processed.push({
          type: 'error',
          data: event.data,
          id: `error-${processed.length}`
        });
      }
    });
    
    // Flush remaining content
    if (contentBuffer.length > 0) {
      processed.push({
        type: 'content',
        data: contentBuffer.join(''),
        id: `content-${processed.length}`
      });
    }
    
    return processed;
  };
  
  // Truncate string for inline display
  const truncateString = (str, maxLength = 40) => {
    if (str.length <= maxLength) return str;
    return str.substring(0, maxLength) + '...';
  };
  
  // Check if arguments need expansion (always show inline preview, but check if needs expand button)
  const needsExpansion = (args) => {
    if (!args) return false;
    
    let argsObj = args;
    
    // If args is a string, try to parse it as JSON
    if (typeof args === 'string') {
      try {
        argsObj = JSON.parse(args);
      } catch (e) {
        // String longer than 100 chars needs expansion to see full content
        return args.length > 100;
      }
    }
    
    // Check if it's a complex object that needs expansion
    if (typeof argsObj === 'object' && argsObj !== null && !Array.isArray(argsObj)) {
      const entries = Object.entries(argsObj);
      
      // More than 3 parameters needs expansion
      if (entries.length > 3) return true;
      
      // Check each value
      for (const [key, value] of entries) {
        if (typeof value === 'string') {
          if (value.length > 80) return true;
        } else if (Array.isArray(value)) {
          if (value.length > 5) return true;
        } else if (typeof value === 'object' && value !== null) {
          if (Object.keys(value).length > 3) return true;
        }
      }
      
      return false;
    }
    
    return false;
  };
  
  // Render arguments inline (for tool header) - always show a preview
  const renderSimpleArgs = (args) => {
    // Check for null, undefined, or empty values
    if (args === null || args === undefined) {
      return <span className="tool-args-inline">{t('message.noArgs')}</span>;
    }
    
    let argsObj = args;
    
    if (typeof args === 'string') {
      // If empty string, show no args
      if (args.trim() === '' || args === '{}') {
        return <span className="tool-args-inline">{t('message.noArgs')}</span>;
      }
      try {
        argsObj = JSON.parse(args);
      } catch (e) {
        // For string args, truncate if too long
        const displayStr = truncateString(args, 60);
        return <span className="tool-args-inline">({displayStr})</span>;
      }
    }
    
    // Handle array case (shouldn't happen but handle it)
    if (Array.isArray(argsObj)) {
      if (argsObj.length === 0) {
        return <span className="tool-args-inline">{t('message.noArgs')}</span>;
      }
      const displayStr = argsObj.slice(0, 3).map(v => 
        typeof v === 'string' ? `"${truncateString(v, 20)}"` : String(v)
      ).join(', ');
      return <span className="tool-args-inline">([{displayStr}{argsObj.length > 3 ? ', ...' : ''}])</span>;
    }
    
    if (typeof argsObj === 'object' && argsObj !== null) {
      const entries = Object.entries(argsObj);

      // If empty object, show a placeholder
      if (entries.length === 0) {
        return <span className="tool-args-inline">{t('message.noArgs')}</span>;
      }
      
      // Limit to first 3 parameters for inline display
      const displayEntries = entries.slice(0, 3);
      const hasMore = entries.length > 3;
      
      const argsStr = displayEntries.map(([key, value]) => {
        let valueStr;
        if (typeof value === 'string') {
          // Truncate long strings
          const truncated = truncateString(value, 40);
          valueStr = `"${truncateString(truncated, 40)}"`;
        } else if (Array.isArray(value)) {
          // Show first few array elements
          const displayItems = value.slice(0, 3);
          const itemsStr = displayItems.map(v => 
            typeof v === 'string' ? `"${truncateString(v, 20)}"` : String(v)
          ).join(', ');
          valueStr = value.length > 3 ? `[${itemsStr}, ...]` : `[${itemsStr}]`;
        } else if (typeof value === 'object' && value !== null) {
          // For objects, show abbreviated form
          const keys = Object.keys(value);
          valueStr = keys.length > 2 ? `{...}` : JSON.stringify(value);
        } else {
          valueStr = String(value);
        }
        return `${key}: ${valueStr}`;
      }).join(', ');
      
      const finalStr = hasMore ? `${argsStr}, ...` : argsStr;
      return <span className="tool-args-inline">({finalStr})</span>;
    }
    
    // Fallback: display as string for other types
    return <span className="tool-args-inline">({String(args)})</span>;
  };
  
  // Render tool arguments helper (for expanded view)
  const renderToolArgs = (args) => {
    if (!args) return null;
    
    let argsObj = args;
    
    // If args is a string, try to parse it as JSON
    if (typeof args === 'string') {
      try {
        argsObj = JSON.parse(args);
      } catch (e) {
        // If parsing fails, display the raw string (full content)
        return (
          <div className="tool-item-arg-block">
            <pre className="tool-arg-text">{args}</pre>
          </div>
        );
      }
    }
    
    // Display as key-value pairs
    if (typeof argsObj === 'object' && argsObj !== null && !Array.isArray(argsObj)) {
      const entries = Object.entries(argsObj);
      
      // If empty object, don't render anything
      if (entries.length === 0) {
        return null;
      }
      
      return entries.map(([key, value]) => {
        // Render value based on type and length
        let displayValue;
        
        if (typeof value === 'string') {
          // For long strings (>80 chars), display in a block format
          if (value.length > 80) {
            displayValue = (
              <pre className="tool-arg-text">{value}</pre>
            );
          } else {
            displayValue = <span className="tool-arg-short">{value}</span>;
          }
        } else if (Array.isArray(value) || typeof value === 'object') {
          // For arrays and objects, format as JSON
          displayValue = (
            <pre className="tool-arg-json">{JSON.stringify(value, null, 2)}</pre>
          );
        } else {
          displayValue = <span className="tool-arg-short">{String(value)}</span>;
        }
        
        return (
          <div key={key} className="tool-item-arg">
            <span className="arg-key">{key}:</span>
            {displayValue}
          </div>
        );
      });
    }
    
    // Fallback: display as formatted JSON
    return (
      <div className="tool-item-arg-block">
        <pre className="tool-arg-json">{JSON.stringify(argsObj, null, 2)}</pre>
      </div>
    );
  };
  
  const processedEvents = !isUser ? processEvents(events) : [];
  
  // Get assistant display name
  const assistantDisplayName = assistantName || t('message.defaultAssistant');
  
  return (
    <div className={`message ${isUser ? 'user-message' : 'assistant-message'} ${isStreaming ? 'streaming' : ''}`}>
      <div className="message-header">
        {isUser ? `👤 ${t('message.you')}` : `🤖 ${assistantDisplayName}`}
        {isStreaming && <span className="streaming-indicator">▊</span>}
      </div>
      
      {/* Assistant message: display events in true chronological order */}
      {!isUser && (
        <div className="message-body">
          {/* Stage indicator (shown when loading, no events yet) */}
          {isStreaming && stage && processedEvents.length === 0 && (
            <div className="stage-indicator">
              {stage === 'thinking' && t('message.stageThinking')}
              {stage === 'tool_calling' && t('message.stageToolCalling')}
              {stage === 'answering' && t('message.stageAnswering')}
            </div>
          )}
          
          {/* Render events in true chronological order */}
          {processedEvents.map((event) => {
            if (event.type === 'error') {
              // Render error event
              const errorData = event.data;
              return (
                <div key={event.id} className="error-event">
                  <div className="error-event-header">
                    <span className="error-icon">⚠️</span>
                    <span className="error-title">{t('message.errorTitle')}</span>
                    {errorData.code && (
                      <span className="error-code">{errorData.code}</span>
                    )}
                  </div>
                  <div className="error-event-body">
                    {errorData.message && (
                      <div className="error-message">{errorData.message}</div>
                    )}
                    {errorData.suggestion && (
                      <div className="error-suggestion">
                        <span className="suggestion-icon">💡</span>
                        <span className="suggestion-text">{errorData.suggestion}</span>
                      </div>
                    )}
                  </div>
                </div>
              );
            } else if (event.type === 'tool_call') {
              const call = event.data;
              const toolId = event.id;
              const isExpanded = expandedTools[toolId];
              // 只有结果需要展开查看
              const hasDetails = call.result && (typeof call.result === 'string' ? call.result.length > 100 : true);
              
              return (
                <div key={event.id} className={`inline-tool-item ${call.status} ${call.phase || ''}`}>
                  <div 
                    className="tool-item-header"
                    onClick={() => hasDetails && toggleTool(toolId)}
                    style={{ cursor: hasDetails ? 'pointer' : 'default' }}
                  >
                    <span className="tool-status-icon">
                      {call.phase === 'start' ? '🚀' : 
                       call.phase === 'fail' ? '❌' :
                       call.status === 'calling' ? '⏳' : 
                       call.status === 'ready' ? '🔧' :
                       call.status === 'failed' ? '❌' :
                       call.success ? '✅' : '❌'}
                    </span>
                    <span className="tool-item-name">{call.tool}</span>
                    {/* 显示阶段标签 */}
                    {call.phase && (
                      <span className="tool-phase-badge">
                        {call.phase === 'start' ? t('message.toolPhaseStart') : 
                         call.phase === 'fail' ? t('message.toolPhaseFail') :
                         t('message.toolPhaseSuccess')}
                      </span>
                    )}
                    {/* 显示工具参数 */}
                    {renderSimpleArgs(call.args)}
                    {hasDetails && (
                      <span className="tool-toggle">
                        {isExpanded ? '▼' : '▶'}
                      </span>
                    )}
                  </div>
                  
                  {isExpanded && (
                    <div className="tool-item-details">
                      {/* 显示工具参数 */}
                      {call.args && Object.keys(call.args).length > 0 && (
                        <div className="tool-detail-section">
                          <div className="tool-detail-label">{t('message.toolParams')}</div>
                          <div className="tool-item-args">
                            {renderToolArgs(call.args)}
                          </div>
                        </div>
                      )}
                      {/* 显示工具结果或错误 */}
                      {call.result && (
                        <div className="tool-detail-section">
                          <div className="tool-detail-label">
                            {call.success !== false ? t('message.toolResult') : t('message.toolError')}
                          </div>
                          <div className={call.success !== false ? "tool-item-result" : "tool-item-error"}>
                            {call.success !== false ? (
                              <pre className="tool-result-content">
                                {typeof call.result === 'string' 
                                  ? call.result 
                                  : call.result.contents ? (
                                    // 处理 contents 数组
                                    Array.isArray(call.result.contents) 
                                      ? call.result.contents.map((content, idx) => {
                                          if (typeof content === 'string') {
                                            return <div key={idx}>{content}</div>;
                                          } else if (content && typeof content === 'object' && content.type === 'text') {
                                            return <div key={idx}>{content.value || ''}</div>;
                                          }
                                          return null;
                                        }).filter(Boolean)
                                      : JSON.stringify(call.result, null, 2)
                                  ) : JSON.stringify(call.result, null, 2)}
                              </pre>
                            ) : (
                              // 显示错误信息
                              <div>
                                {call.result.error ? (
                                  call.result.error
                                ) : call.result.contents ? (
                                  // 如果有 contents，尝试提取错误信息
                                  Array.isArray(call.result.contents) 
                                    ? call.result.contents.map((content, idx) => {
                                        if (typeof content === 'string') {
                                          return <div key={idx}>{content}</div>;
                                        } else if (content && typeof content === 'object' && content.type === 'text') {
                                          return <div key={idx}>{content.value || ''}</div>;
                                        }
                                        return null;
                                      }).filter(Boolean)
                                    : JSON.stringify(call.result.contents)
                                ) : (
                                  t('message.toolCallFailed')
                                )}
                              </div>
                            )}
                          </div>
                        </div>
                      )}
                      {/* 如果没有 result 但是状态是失败，显示失败提示 */}
                      {!call.result && call.phase === 'fail' && (
                        <div className="tool-detail-section">
                          <div className="tool-detail-label">{t('message.toolError')}</div>
                          <div className="tool-item-error">
                            {t('message.toolCallFailed')}
                          </div>
                        </div>
                      )}
                    </div>
                  )}
                </div>
              );
            } else if (event.type === 'content') {
              // Parse content for <think></think> blocks
              const contentParts = parseThinkingBlocks(event.data);
              
              return (
                <div key={event.id} className="message-content-wrapper">
                  {contentParts.map((part, idx) => {
                    if (part.type === 'thinking') {
                      const thinkId = `${event.id}-think-${part.id}`;
                      const isExpanded = expandedThinking[thinkId];
                      
                      return (
                        <div key={idx} className="thinking-block">
                          <div 
                            className="thinking-header"
                            onClick={() => toggleThinking(thinkId)}
                          >
                            <span className="thinking-icon">
                              {isExpanded ? '🧠' : '💭'}
                            </span>
                            <span className="thinking-title">
                              {isExpanded ? t('message.thinkingExpanded') : t('message.thinkingCollapsed')}
                            </span>
                            <span className="thinking-toggle">
                              {isExpanded ? '▼' : '▶'}
                            </span>
                          </div>
                          {isExpanded && (
                            <div className="thinking-content">
                              <ReactMarkdown remarkPlugins={[remarkGfm]} components={{ a: LinkRenderer }}>{part.content}</ReactMarkdown>
                            </div>
                          )}
                        </div>
                      );
                    } else {
                      return (
                        <div key={idx} className="message-content-block">
                          <ReactMarkdown remarkPlugins={[remarkGfm]} components={{ a: LinkRenderer }}>{part.content}</ReactMarkdown>
                        </div>
                      );
                    }
                  })}
                </div>
              );
            }
            return null;
          })}
          
          {/* Stage indicator during streaming */}
          {isStreaming && stage && processedEvents.length > 0 && (
            <div className="stage-indicator-inline">
              {stage === 'tool_calling' && t('message.stageToolCallingInline')}
              {stage === 'answering' && t('message.stageAnsweringInline')}
            </div>
          )}
          
          {/* Feedback and Copy buttons - show when not streaming and has content */}
          {!isStreaming && processedEvents.length > 0 && (
            <div className="feedback-container">
              <div className="feedback-buttons">
                {/* Only show feedback buttons (like/dislike) in non-shared conversations */}
                {!isShared && conversationId && requestId && (
                  <>
                    <button 
                      className={`feedback-btn like-btn ${feedbackStatus === 'like' ? 'active' : ''} ${feedbackStatus ? 'disabled' : ''}`}
                      onClick={handleLike}
                      disabled={!!feedbackStatus || isSubmittingFeedback}
                      title={t('message.helpfulTitle')}
                    >
                      👍 {feedbackStatus === 'like' ? t('message.liked') : t('message.helpful')}
                    </button>
                    <button 
                      className={`feedback-btn dislike-btn ${feedbackStatus === 'dislike' ? 'active' : ''} ${feedbackStatus ? 'disabled' : ''}`}
                      onClick={handleDislike}
                      disabled={!!feedbackStatus || isSubmittingFeedback}
                      title={t('message.improvementTitle')}
                    >
                      👎 {feedbackStatus === 'dislike' ? t('message.feedbackSent') : t('message.needsImprovement')}
                    </button>
                  </>
                )}
                {/* Always show copy button when there's content */}
                <button 
                  className={`feedback-btn copy-btn ${copySuccess ? 'active' : ''}`}
                  onClick={handleCopy}
                  title={t('message.copyTitle')}
                >
                  {copySuccess ? `✓ ${t('message.copied')}` : `📋 ${t('message.copy')}`}
                </button>
              </div>
              {/* Thank you message after feedback */}
              {!isShared && feedbackStatus && (
                <span className="feedback-message">
                  {t('message.thanksFeedback')}
                </span>
              )}
            </div>
          )}
        </div>
      )}
      
      {/* Feedback Modal for dislike reason */}
      {showFeedbackModal && (
        <div className="feedback-modal-overlay" onClick={handleCloseModal}>
          <div className="feedback-modal" onClick={e => e.stopPropagation()}>
            <div className="feedback-modal-header">
              <h3>{t('message.feedbackModalTitle')}</h3>
              <button className="feedback-modal-close" onClick={handleCloseModal}>×</button>
            </div>
            <div className="feedback-modal-body">
              <textarea
                className="feedback-textarea"
                placeholder={t('message.feedbackPlaceholder')}
                value={feedbackReason}
                onChange={e => setFeedbackReason(e.target.value)}
                rows={4}
              />
            </div>
            <div className="feedback-modal-footer">
              <button 
                className="feedback-modal-cancel" 
                onClick={handleCloseModal}
                disabled={isSubmittingFeedback}
              >
                {t('common.cancel')}
              </button>
              <button 
                className="feedback-modal-submit" 
                onClick={handleDislikeSubmit}
                disabled={isSubmittingFeedback}
              >
                {isSubmittingFeedback ? t('message.submitting') : t('message.submitFeedback')}
              </button>
            </div>
          </div>
        </div>
      )}
      
      {/* User message content */}
      {isUser && (
        <div className="message-content">
          {images.length > 0 && (
            <div className="user-images-container">
              {images.map((img, index) => (
                <div key={index} className="user-image-preview" onClick={() => handleImageClick(index)}>
                  <img src={img} alt={`${t('message.userImage')} ${index + 1}`} />
                  <div className="image-zoom-hint">{t('message.clickToEnlarge')}</div>
                  {images.length > 1 && (
                    <div className="image-number-badge">{index + 1}/{images.length}</div>
                  )}
                </div>
              ))}
            </div>
          )}
          {content && <p>{content}</p>}
        </div>
      )}
      
      {/* Image preview modal */}
      {showImagePreview && images.length > 0 && (
        <div className="image-preview-modal" onClick={handleCloseImagePreview}>
          <div className="image-preview-modal-content" onClick={(e) => e.stopPropagation()}>
            <button className="image-preview-close" onClick={handleCloseImagePreview}>×</button>
            {images.length > 1 && (
              <>
                <button className="image-preview-nav prev" onClick={handlePrevImage}>‹</button>
                <button className="image-preview-nav next" onClick={handleNextImage}>›</button>
                <div className="image-preview-counter">
                  {previewImageIndex + 1} / {images.length}
                </div>
              </>
            )}
            <img src={images[previewImageIndex]} alt={`${t('message.imagePreview')} ${previewImageIndex + 1}`} className="image-preview-full" />
          </div>
        </div>
      )}
    </div>
  );
};

export default Message;

