/**
 * ShareChat Component
 * Displays a shared conversation by threadId (read-only)
 */
import React, { useState, useEffect } from 'react';
import { useParams, useSearchParams } from 'react-router-dom';
import Message from './Message';
import { getSharedThread, getSharedThreadMessages, getSharedEmployee } from '../services/api';

const ShareChat = () => {
  const { employeeName, threadId } = useParams();
  const [searchParams] = useSearchParams();
  const [employee, setEmployee] = useState(null);
  const [thread, setThread] = useState(null);
  const [messages, setMessages] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const cloudAccountId = searchParams.get('cloudAccountId') || '';

  useEffect(() => {
    const loadData = async () => {
      try {
        setLoading(true);
        setError(null);

        // Load thread info (public API, no auth required)
        const threadData = await getSharedThread(employeeName, threadId, cloudAccountId);
        setThread(threadData);

        // Load employee info (public API, no auth required)
        const employeeData = await getSharedEmployee(employeeName, cloudAccountId);
        setEmployee(employeeData);

        // Load messages (public API, no auth required)
        const messagesData = await getSharedThreadMessages(employeeName, threadId, cloudAccountId);
        
        // Convert backend message format to frontend format
        const convertedMessages = convertMessages(messagesData);
        setMessages(convertedMessages);
      } catch (err) {
        console.error('Failed to load shared conversation:', err);
        setError(err.message || '加载对话失败');
      } finally {
        setLoading(false);
      }
    };

    if (employeeName && threadId) {
      loadData();
    }
  }, [employeeName, threadId, cloudAccountId]);

  /**
   * Convert backend message format to frontend format
   * Backend format: { role: string, contents: [{ type: string, value: string }], tools: [...] }
   * Frontend format: { role: string, content?: string, events?: Array }
   * 
   * Note: Backend may return multiple messages for the same assistant response (one per content chunk).
   * We need to merge consecutive messages with the same role into a single message.
   */
  const convertMessages = (backendMessages) => {
    const converted = [];
    let currentMessage = null;
    
    for (const msg of backendMessages) {
      const role = msg.role || 'user';
      
      if (role === 'user') {
        // User message: always create a new message
        const textContent = extractTextContent(msg.contents || []);
        if (textContent) {
          // Flush current assistant message if exists
          if (currentMessage && currentMessage.role === 'assistant') {
            converted.push(currentMessage);
            currentMessage = null;
          }
          converted.push({
            role: 'user',
            content: textContent
          });
        }
      } else {
        // Assistant message: merge with current message if same role
        if (currentMessage && currentMessage.role === 'assistant') {
          // Merge into existing assistant message
          const events = currentMessage.events || [];
          
          // Process tools first (they should appear before content)
          if (msg.tools && Array.isArray(msg.tools) && msg.tools.length > 0) {
            for (const tool of msg.tools) {
              const toolName = tool.name || 'unknown_tool';
              const toolStatus = tool.status || 'completed';
              const toolArgs = tool.arguments || {};
              const toolContents = tool.contents || [];
              
              // Determine tool call phase and status first
              let phase = 'success';
              let status = 'completed';
              let toolSuccess = true;
              
              if (toolStatus === 'start') {
                phase = 'start';
                status = 'calling';
              } else if (toolStatus === 'success' || toolStatus === 'completed') {
                phase = 'success';
                status = 'ready';
              } else if (toolStatus === 'fail' || toolStatus === 'error' || toolStatus === 'failed') {
                phase = 'fail';
                status = 'failed';
                toolSuccess = false;
              }
              
              // Extract tool result - format consistent with ChatWindow
              let toolResult = null;
              
              if (toolContents && toolContents.length > 0) {
                toolResult = {
                  success: toolSuccess,
                  contents: toolContents
                };
                
                if (!toolSuccess) {
                  const errorTexts = toolContents
                    .filter(c => c.type === 'text')
                    .map(c => c.value || '')
                    .join('');
                  
                  if (errorTexts) {
                    toolResult.error = errorTexts;
                  } else {
                    toolResult.error = '工具调用失败';
                  }
                }
              } else if (!toolSuccess) {
                toolResult = {
                  success: false,
                  error: '工具调用失败'
                };
              }
              
              events.push({
                type: 'tool_call',
                data: {
                  tool: toolName,
                  args: toolArgs,
                  status: status,
                  phase: phase,
                  result: toolResult,
                  success: toolSuccess
                },
                timestamp: Date.now()
              });
            }
          }
          
          // Process text contents - add as content events
          if (msg.contents && msg.contents.length > 0) {
            for (const content of msg.contents) {
              const contentType = content.type || '';
              const contentValue = content.value || '';
              
              if (contentType === 'text' && contentValue) {
                events.push({
                  type: 'content',
                  data: contentValue,
                  timestamp: Date.now()
                });
              }
            }
          }
          
          currentMessage.events = events;
        } else {
          // Create new assistant message
          const events = [];
          
          // Process tools first
          if (msg.tools && Array.isArray(msg.tools) && msg.tools.length > 0) {
            for (const tool of msg.tools) {
              const toolName = tool.name || 'unknown_tool';
              const toolStatus = tool.status || 'completed';
              const toolArgs = tool.arguments || {};
              const toolContents = tool.contents || [];
              
              let phase = 'success';
              let status = 'completed';
              let toolSuccess = true;
              
              if (toolStatus === 'start') {
                phase = 'start';
                status = 'calling';
              } else if (toolStatus === 'success' || toolStatus === 'completed') {
                phase = 'success';
                status = 'ready';
              } else if (toolStatus === 'fail' || toolStatus === 'error' || toolStatus === 'failed') {
                phase = 'fail';
                status = 'failed';
                toolSuccess = false;
              }
              
              let toolResult = null;
              
              if (toolContents && toolContents.length > 0) {
                toolResult = {
                  success: toolSuccess,
                  contents: toolContents
                };
                
                if (!toolSuccess) {
                  const errorTexts = toolContents
                    .filter(c => c.type === 'text')
                    .map(c => c.value || '')
                    .join('');
                  
                  if (errorTexts) {
                    toolResult.error = errorTexts;
                  } else {
                    toolResult.error = '工具调用失败';
                  }
                }
              } else if (!toolSuccess) {
                toolResult = {
                  success: false,
                  error: '工具调用失败'
                };
              }
              
              events.push({
                type: 'tool_call',
                data: {
                  tool: toolName,
                  args: toolArgs,
                  status: status,
                  phase: phase,
                  result: toolResult,
                  success: toolSuccess
                },
                timestamp: Date.now()
              });
            }
          }
          
          // Process text contents
          if (msg.contents && msg.contents.length > 0) {
            for (const content of msg.contents) {
              const contentType = content.type || '';
              const contentValue = content.value || '';
              
              if (contentType === 'text' && contentValue) {
                events.push({
                  type: 'content',
                  data: contentValue,
                  timestamp: Date.now()
                });
              }
            }
          }
          
          // If no events but has content, create a content event
          if (events.length === 0) {
            const textContent = extractTextContent(msg.contents || []);
            if (textContent) {
              events.push({
                type: 'content',
                data: textContent,
                timestamp: Date.now()
              });
            }
          }
          
          if (events.length > 0) {
            currentMessage = {
              role: 'assistant',
              events: events
            };
          }
        }
      }
    }
    
    // Flush last message if exists
    if (currentMessage) {
      converted.push(currentMessage);
    }
    
    return converted;
  };

  /**
   * Extract text content from contents array
   */
  const extractTextContent = (contents) => {
    if (!contents || contents.length === 0) return '';
    
    const textParts = contents
      .filter(c => c.type === 'text')
      .map(c => c.value || '')
      .join('');
    
    return textParts;
  };

  if (loading) {
    return (
      <div className="chat-window chat-window-shared">
        <div className="loading-center">
          <div className="loading-spinner"></div>
          <p>正在加载对话...</p>
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="chat-window chat-window-shared">
        <div className="error-center">
          <h3>加载失败</h3>
          <p>{error}</p>
        </div>
      </div>
    );
  }

  if (!employee || !thread) {
    return null;
  }

  return (
    <div className="chat-window chat-window-shared">
      <div className="chat-header chat-header-shared">
        <div className="header-left">
          <h2>分享的对话</h2>
          <span className="read-only-badge">只读</span>
        </div>
        <div className="header-center">
          <span className="thread-id">ID: {threadId}</span>
        </div>
        <div className="header-buttons">
          <button 
            className="new-session-button-shared"
            onClick={() => {
              // 在新窗口打开该SOP问答助手的新会话页面
              if (employeeName) {
                const params = new URLSearchParams();
                if (cloudAccountId) {
                  params.set('cloudAccountId', cloudAccountId);
                }
                window.open(`/#/chat/${employeeName}${params.toString() ? `?${params.toString()}` : ''}`, '_blank');
              } else {
                // 如果没有员工信息，打开首页
                window.open('/', '_blank');
              }
            }}
          >
            创建新会话
          </button>
        </div>
      </div>

      <div className="messages-container">
        {messages.length === 0 ? (
          <div className="welcome-message">
            <h3>此对话暂无消息</h3>
          </div>
        ) : (
          messages.map((message, index) => (
            <Message 
              key={index} 
              role={message.role} 
              content={message.content}
              events={message.events}
              isShared={true}
              assistantName={employee?.displayName || employee?.name}
            />
          ))
        )}
      </div>
    </div>
  );
};

export default ShareChat;
