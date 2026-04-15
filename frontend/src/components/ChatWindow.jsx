/**
 * ChatWindow Component
 * Main chat interface with message history
 *
 * TODO: 重构工具调用处理逻辑
 * 当前问题：
 * 1. 与后端SSE事件协议耦合过紧
 * 2. 状态管理复杂，容易出错
 * 3. 调试代码污染业务逻辑
 *
 * 建议解决方案：
 * 1. 创建独立的ToolCallManager类处理工具调用状态
 * 2. 使用useReducer管理复杂状态
 * 3. 将调试逻辑移到开发环境配置中
 */
import React, { useState, useEffect, useRef, useCallback } from 'react';
import { useParams, useNavigate, useSearchParams } from 'react-router-dom';
import Message from './Message';
import MessageInput from './MessageInput';
import { sendChatMessageStream, createThread, getEmployee, listThreads, getThreadMessages } from '../services/api';
import { copyToClipboard } from '../utils/clipboard';


const ChatWindow = () => {
  const { employeeId } = useParams();
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const [employee, setEmployee] = useState(null);
  const [employeeLoading, setEmployeeLoading] = useState(true);
  const [messages, setMessages] = useState([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState(null);
  const [pendingCloudAccountConfirm, setPendingCloudAccountConfirm] = useState(null);
  const [streamingMessage, setStreamingMessage] = useState(null);
  const [threadId, setThreadId] = useState(null);
  const [autoScroll, setAutoScroll] = useState(true); // 是否自动滚动
  const [shareCopied, setShareCopied] = useState(false); // 分享链接是否已复制
  const [threads, setThreads] = useState([]); // Thread 列表
  const [threadsLoading, setThreadsLoading] = useState(false); // Thread 列表加载状态
  const [sidebarOpen, setSidebarOpen] = useState(window.innerWidth > 768); // 侧边栏是否打开（移动端默认关闭）
  const messagesEndRef = useRef(null);
  const messagesContainerRef = useRef(null);
  const streamingMessageRef = useRef(null);
  const abortControllerRef = useRef(null);
  const userInteractingRef = useRef(false); // 追踪用户是否手动滚动
  const scrollTimeoutRef = useRef(null);
  const lastScrollTopRef = useRef(0);
  const shareTimeoutRef = useRef(null);
  const requestedCloudAccountId = searchParams.get('cloudAccountId') || '';
  const selectedCloudAccountId = employee?.cloudAccountId || requestedCloudAccountId;

  // Load employee data on mount
  useEffect(() => {
    const loadEmployee = async () => {
      try {
        setEmployeeLoading(true);
        const employeeData = await getEmployee(employeeId, requestedCloudAccountId);
        setEmployee(employeeData);
      } catch (err) {
        console.error('Failed to load employee:', err);
        // If employee not found, redirect to employee selector
        navigate('/');
      } finally {
        setEmployeeLoading(false);
      }
    };

    if (employeeId) {
      loadEmployee();
    }
  }, [employeeId, navigate, requestedCloudAccountId]);

  // Function to load and refresh thread list (only top 10)
  const refreshThreadList = useCallback(async () => {
    if (!employee) return;
    
    try {
      setThreadsLoading(true);
      const threadList = await listThreads(employee.name, selectedCloudAccountId);
      // Sort by createTime descending (newest first) and take only top 10
      const sortedThreads = threadList.sort((a, b) => {
        const timeA = a.createTime || 0;
        const timeB = b.createTime || 0;
        return timeB - timeA;
      }).slice(0, 10); // Only take first 10 threads
      setThreads(sortedThreads);
    } catch (err) {
      console.error('Failed to load threads:', err);
    } finally {
      setThreadsLoading(false);
    }
  }, [employee, selectedCloudAccountId]);

  // Load thread list when employee is loaded
  useEffect(() => {
    refreshThreadList();
  }, [refreshThreadList]);

  // Check if scrolled to bottom
  const checkIfAtBottom = () => {
    const container = messagesContainerRef.current;
    if (!container) return true;
    
    const threshold = 100; // 100px threshold
    const isBottom = container.scrollHeight - container.scrollTop - container.clientHeight < threshold;
    return isBottom;
  };

  // Handle scroll event - detect user manual scrolling
  const handleScroll = () => {
    const container = messagesContainerRef.current;
    if (!container) return;

    const currentScrollTop = container.scrollTop;
    const isBottom = checkIfAtBottom();
    
    // Detect if user scrolled up manually (not auto-scroll)
    if (currentScrollTop < lastScrollTopRef.current && !isBottom) {
      // User scrolled up
      userInteractingRef.current = true;
      setAutoScroll(false);
      
      // Clear any existing timeout
      if (scrollTimeoutRef.current) {
        clearTimeout(scrollTimeoutRef.current);
      }
      
      // Reset interaction flag after user stops scrolling
      scrollTimeoutRef.current = setTimeout(() => {
        userInteractingRef.current = false;
        // If user scrolled back to bottom, re-enable auto scroll
        if (checkIfAtBottom()) {
          setAutoScroll(true);
        }
      }, 200);
    } else if (isBottom && !userInteractingRef.current) {
      // User at bottom (or auto-scrolled to bottom)
      setAutoScroll(true);
    }
    
    lastScrollTopRef.current = currentScrollTop;
  };

  // Auto scroll to bottom when new content arrives
  useEffect(() => {
    if (autoScroll && !userInteractingRef.current) {
      const container = messagesContainerRef.current;
      if (container) {
        // Use scrollTo for smooth auto-scroll
        container.scrollTo({
          top: container.scrollHeight,
          behavior: 'auto' // Use 'auto' for instant scroll during streaming
        });
      }
    }
  }, [messages, streamingMessage, autoScroll]);

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      if (scrollTimeoutRef.current) {
        clearTimeout(scrollTimeoutRef.current);
      }
      if (shareTimeoutRef.current) {
        clearTimeout(shareTimeoutRef.current);
      }
    };
  }, []);

  const scrollToBottom = () => {
    const container = messagesContainerRef.current;
    if (container) {
      container.scrollTo({
        top: container.scrollHeight,
        behavior: 'smooth'
      });
      setAutoScroll(true);
      userInteractingRef.current = false;
    }
  };

  /**
   * Convert backend message format to frontend format
   * Backend format: { role: string, contents: [{ type: string, value: string }], tools: [...] }
   * Frontend format: { role: string, content?: string, events?: Array }
   */
  const convertMessages = (backendMessages) => {
    const converted = [];
    let currentMessage = null;
    
    const extractTextContent = (contents) => {
      if (!contents || contents.length === 0) return '';
      const textParts = contents
        .filter(c => c.type === 'text')
        .map(c => c.value || '')
        .join('');
      return textParts;
    };
    
    for (const msg of backendMessages) {
      const role = msg.role || 'user';
      
      if (role === 'user') {
        const textContent = extractTextContent(msg.contents || []);
        if (textContent) {
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
        if (currentMessage && currentMessage.role === 'assistant') {
          const events = currentMessage.events || [];
          
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
                  toolResult.error = errorTexts || '工具调用失败';
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
          
          if (msg.contents && msg.contents.length > 0) {
            for (const content of msg.contents) {
              if (content.type === 'text' && content.value) {
                events.push({
                  type: 'content',
                  data: content.value,
                  timestamp: Date.now()
                });
              }
            }
          }
          
          currentMessage.events = events;
        } else {
          const events = [];
          
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
                  toolResult.error = errorTexts || '工具调用失败';
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
          
          if (msg.contents && msg.contents.length > 0) {
            for (const content of msg.contents) {
              if (content.type === 'text' && content.value) {
                events.push({
                  type: 'content',
                  data: content.value,
                  timestamp: Date.now()
                });
              }
            }
          }
          
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
    
    if (currentMessage) {
      converted.push(currentMessage);
    }
    
    return converted;
  };

  // Handle thread selection
  const handleThreadSelect = async (selectedThreadId) => {
    if (selectedThreadId === threadId) return;
    
    if (loading) {
      // If currently loading, abort the request
      if (abortControllerRef.current) {
        abortControllerRef.current.abort();
      }
    }
    
    try {
      setLoading(true);
      setError(null);
      setPendingCloudAccountConfirm(null);
      setMessages([]);
      setStreamingMessage(null);
      
      // Load messages for the selected thread
      const backendMessages = await getThreadMessages(employee.name, selectedThreadId, selectedCloudAccountId);
      const convertedMessages = convertMessages(backendMessages);
      setMessages(convertedMessages);
      setThreadId(selectedThreadId);
      
      // Scroll to bottom after loading
      setTimeout(() => {
        scrollToBottom();
      }, 100);
    } catch (err) {
      setError('加载会话失败: ' + err.message);
      console.error('Failed to load thread messages:', err);
    } finally {
      setLoading(false);
    }
  };

  // Handle new thread creation
  const handleNewThread = () => {
    setMessages([]);
    setThreadId(null);
    setError(null);
    setPendingCloudAccountConfirm(null);
    setStreamingMessage(null);
  };

  const handleSendMessage = async (content) => {
    const pendingConfirmation = pendingCloudAccountConfirm;
    const isCloudAccountConfirmation = Boolean(pendingConfirmation);

    // Create abort controller for this request
    abortControllerRef.current = new AbortController();
    setError(null);
    setLoading(true);

    if (!isCloudAccountConfirmation) {
      // Add user message to chat only for real questions.
      // 环境确认属于本地路由步骤，不写入会话历史，避免与后端 thread 历史不一致。
      const userMessage = {
        role: 'user',
        content
      };
      setMessages((prev) => [...prev, userMessage]);
    } else {
      setPendingCloudAccountConfirm(null);
    }

    // Reset scroll state for new message
    setAutoScroll(true);
    userInteractingRef.current = false;

    let currentThreadId = pendingConfirmation?.threadId || threadId;
    const requestMessage = pendingConfirmation?.originalMessage || content;
    const requestCloudAccountId = isCloudAccountConfirmation ? content : selectedCloudAccountId;

    if (!currentThreadId) {
      try {
        const threadData = await createThread(employee.name, '', {}, selectedCloudAccountId);
        currentThreadId = threadData.threadId;
        setThreadId(currentThreadId);
        // Reload thread list after creating new thread
        refreshThreadList();
      } catch (err) {
        setError('创建会话失败: ' + err.message);
        setLoading(false);
        setStreamingMessage(null);
        return;
      }
    }

    // Initialize streaming message
    streamingMessageRef.current = {
      role: 'assistant',
      events: [],  // Store all events (content chunks, tool calls) in chronological order
      stage: 'thinking'
    };
    setStreamingMessage(streamingMessageRef.current);

    // Send to backend with SSE streaming
    await sendChatMessageStream(
      employee.name,
      currentThreadId,
      requestMessage,
      requestCloudAccountId,
      // onMeta: handle meta info
      (receivedThreadId) => {
        if (receivedThreadId && !threadId) {
          setThreadId(receivedThreadId);
        }
      },
      // onChunk: handle incoming content chunks
      // 注意：新的 API 格式每次发送的是累积的完整内容，所以应该更新最后一个 content 事件
      (chunk) => {
        const events = streamingMessageRef.current.events;
        
        // 如果最后一个事件是 content 类型，更新它；否则添加新事件
        if (events.length > 0 && events[events.length - 1].type === 'content') {
          // 更新最后一个 content 事件
          events[events.length - 1] = {
            type: 'content',
            data: chunk,
            timestamp: Date.now()
          };
        } else {
          // 添加新的 content 事件
          events.push({
            type: 'content',
            data: chunk,
            timestamp: Date.now()
          });
        }

        streamingMessageRef.current.events = events;
        streamingMessageRef.current.stage = 'answering';
        setStreamingMessage({ ...streamingMessageRef.current, events: [...events] });
      },
      // onToolCall: handle tool call
      (toolName, args, status) => {
        // 使用 API 返回的 status 字段（start、success 或 fail）
        const phase = status === 'start' ? 'start' : (status === 'fail' ? 'fail' : 'success');
        
        if (status === 'start') {
          // Start 阶段：添加新事件
          // 确保 args 是对象格式，如果为空则使用空对象
          let normalizedArgs = {};
          
          if (args) {
            if (typeof args === 'object' && !Array.isArray(args)) {
              // 已经是对象格式，直接使用
              normalizedArgs = args;
            } else if (typeof args === 'string') {
              // 如果是字符串，尝试解析 JSON
              try {
                normalizedArgs = JSON.parse(args);
              } catch (e) {
                // 解析失败，使用原始值
                normalizedArgs = { value: args };
              }
            } else {
              // 其他类型，包装成对象
              normalizedArgs = { value: args };
            }
          }
          
          streamingMessageRef.current.events.push({
            type: 'tool_call',
            data: {
              tool: toolName,
              args: normalizedArgs,
              status: 'calling',
              phase: 'start'
            },
            timestamp: Date.now()
          });
        } else if (status === 'fail') {
          // Fail 阶段：查找对应的 start 事件并更新，如果找不到则创建新事件
          const events = streamingMessageRef.current.events;
          let found = false;
          
          // 从后往前查找最后一个 start 阶段的同名工具调用
          for (let i = events.length - 1; i >= 0; i--) {
            if (events[i].type === 'tool_call' &&
                events[i].data.tool === toolName &&
                events[i].data.phase === 'start') {
              // 更新现有事件：保留 start 阶段的 args，更新状态为失败
              const startArgs = events[i].data.args;
              const hasStartArgs = startArgs && typeof startArgs === 'object' && Object.keys(startArgs).length > 0;
              const finalArgs = hasStartArgs ? startArgs : (args && typeof args === 'object' && Object.keys(args).length > 0 ? args : {});
              
              events[i] = {
                ...events[i],
                data: {
                  ...events[i].data,
                  args: finalArgs,
                  status: 'failed',
                  phase: 'fail'
                }
              };
              found = true;
              break;
            }
          }
          
          // 如果没找到 start 事件，创建新事件（可能 start 事件丢失了）
          if (!found) {
            streamingMessageRef.current.events.push({
              type: 'tool_call',
              data: {
                tool: toolName,
                args: args || {},
                status: 'failed',
                phase: 'fail'
              },
              timestamp: Date.now()
            });
          }
        } else {
          // Success 阶段：查找对应的 start 事件并更新，如果找不到则创建新事件
          const events = streamingMessageRef.current.events;
          let found = false;
          
          // 从后往前查找最后一个 start 阶段的同名工具调用
          for (let i = events.length - 1; i >= 0; i--) {
            if (events[i].type === 'tool_call' &&
                events[i].data.tool === toolName &&
                events[i].data.phase === 'start') {
              // 更新现有事件：保留 start 阶段的 args，更新状态
              // 优先使用 start 阶段的 args，如果 start 阶段的 args 为空对象或没有，则使用 success 阶段的 args
              const startArgs = events[i].data.args;
              const hasStartArgs = startArgs && typeof startArgs === 'object' && Object.keys(startArgs).length > 0;
              const finalArgs = hasStartArgs ? startArgs : (args && typeof args === 'object' && Object.keys(args).length > 0 ? args : {});
              
              events[i] = {
                ...events[i],
                data: {
                  ...events[i].data,
                  args: finalArgs,
                  status: 'ready',
                  phase: 'success'
                }
              };
              found = true;
              break;
            }
          }
          
          // 如果没找到 start 事件，创建新事件（可能 start 事件丢失了）
          if (!found) {
            streamingMessageRef.current.events.push({
              type: 'tool_call',
              data: {
                tool: toolName,
                args: args || {},
                status: 'ready',
                phase: 'success'
              },
              timestamp: Date.now()
            });
          }
        }

        streamingMessageRef.current.stage = 'tool_calling';
        setStreamingMessage({...streamingMessageRef.current});
      },
      // onToolResult: handle tool result
      (toolName, result) => {
        // 找到对应的tool_call事件并更新状态（找最后一个 success 或 fail 阶段的）
        const events = streamingMessageRef.current.events;
        for (let i = events.length - 1; i >= 0; i--) {
          if (events[i].type === 'tool_call' &&
              events[i].data.tool === toolName &&
              (events[i].data.phase === 'success' || events[i].data.phase === 'fail')) {
            const isSuccess = result.success !== false;
            events[i] = {
              ...events[i],
              data: {
                ...events[i].data,
                status: isSuccess ? 'completed' : 'failed',
                result: result,
                success: isSuccess
              }
            };
            break;
          }
        }

        streamingMessageRef.current.stage = null;
        setStreamingMessage({...streamingMessageRef.current});
      },
      // onErrorEvent: handle error events in stream
      (errorPayload) => {
        console.log('[ChatWindow] Error event callback triggered:', errorPayload);
        // Add error event to the events array
        streamingMessageRef.current.events.push({
          type: 'error',
          data: {
            code: errorPayload.code || 'UNKNOWN_ERROR',
            message: errorPayload.message || '发生未知错误',
            suggestion: errorPayload.suggestion || ''
          },
          timestamp: Date.now()
        });
        setStreamingMessage({...streamingMessageRef.current});
      },
      // onComplete: finalize the message
      () => {
        // Save the complete message with events array
        const finalMessage = {
          role: 'assistant',
          events: streamingMessageRef.current.events  // Save all events in chronological order
        };

        setMessages((prev) => [...prev, finalMessage]);

        // Refresh thread list after chat completion
        refreshThreadList();

        // Clear streaming state
        setTimeout(() => {
          setStreamingMessage(null);
          streamingMessageRef.current = null;
          setLoading(false);
        }, 0);
      },
      // onError: handle errors
      (err) => {
        if (err.needConfirm) {
          const options = Array.isArray(err.options) ? err.options : [];
          setPendingCloudAccountConfirm({
            originalMessage: pendingConfirmation?.originalMessage || content,
            threadId: currentThreadId,
            options,
          });
          setError(null);
          setStreamingMessage(null);
          streamingMessageRef.current = null;
          setLoading(false);
          abortControllerRef.current = null;
          return;
        }

        const isCancelled = err.message.includes('取消');

        if (isCancelled) {
          // User cancelled - preserve any content received so far
          if (streamingMessageRef.current && streamingMessageRef.current.events && streamingMessageRef.current.events.length > 0) {
            // Add a cancellation notice to the events
            streamingMessageRef.current.events.push({
              type: 'content',
              data: '\n\n_[已停止生成]_',
              timestamp: Date.now()
            });
            
            const partialMessage = {
              role: 'assistant',
              events: streamingMessageRef.current.events
            };
            setMessages((prev) => [...prev, partialMessage]);
          } else {
            // No content received yet, show cancellation message
            const cancelMessage = {
              role: 'assistant',
              content: '生成已被用户取消。'
            };
            setMessages((prev) => [...prev, cancelMessage]);
          }
        } else {
          // Other errors - show error message
          const errorMessage = {
            role: 'assistant',
            content: `**发生错误**\n\n${err.message}`
          };
          setMessages((prev) => [...prev, errorMessage]);
        }
        
        setStreamingMessage(null);
        streamingMessageRef.current = null;
        setLoading(false);
        abortControllerRef.current = null;
      },
      // Pass abort signal
      abortControllerRef.current?.signal
    );
  };

  const handleStopGeneration = () => {
    if (abortControllerRef.current) {
      abortControllerRef.current.abort();
      abortControllerRef.current = null;
    }
  };

  const handleBack = () => {
    navigate('/');
  };

  const handleShare = () => {
    if (!threadId) {
      alert('当前对话还没有保存，无法分享');
      return;
    }

    if (!employee?.name) {
      alert('员工信息未加载完成，无法分享');
      return;
    }
    
    // Generate share URL
    const baseUrl = window.location.href.split('#')[0];
    const shareParams = new URLSearchParams();
    if (selectedCloudAccountId) {
      shareParams.set('cloudAccountId', selectedCloudAccountId);
    }
    const shareUrl = `${baseUrl}#/share/${encodeURIComponent(employee.name)}/${encodeURIComponent(threadId)}${shareParams.toString() ? `?${shareParams.toString()}` : ''}`;

    // Copy to clipboard (robust fallback across browsers / contexts)
    (async () => {
      try {
        const ok = await copyToClipboard(shareUrl);
        if (!ok) {
          throw new Error('copy_failed');
        }

        setShareCopied(true);
        // 清除之前的定时器
        if (shareTimeoutRef.current) {
          clearTimeout(shareTimeoutRef.current);
        }
        // 2秒后恢复原状
        shareTimeoutRef.current = setTimeout(() => {
          setShareCopied(false);
        }, 2000);
      } catch (err) {
        console.error('Failed to copy share URL:', err);
        // Fallback: show the URL in a prompt
        prompt('分享链接（请手动复制）：', shareUrl);
      }
    })();
  };

  // Show loading while employee data is being fetched
  if (employeeLoading) {
    return (
      <div className="chat-window">
        <div className="loading-center">
          <div className="loading-spinner"></div>
          <p>正在加载员工信息...</p>
        </div>
      </div>
    );
  }

  // If employee not found, component will redirect in useEffect
  if (!employee) {
    return null;
  }

  // Format thread title for display
  const formatThreadTitle = (thread) => {
    if (thread.title && thread.title.trim()) {
      return thread.title;
    }
    if (thread.createTime) {
      const date = new Date(thread.createTime);
      return date.toLocaleString('zh-CN', { 
        month: 'short', 
        day: 'numeric', 
        hour: '2-digit', 
        minute: '2-digit' 
      });
    }
    return '新对话';
  };

  const cloudAccountOptionsText = pendingCloudAccountConfirm?.options?.join(' / ') || '';
  const messageInputPlaceholder = pendingCloudAccountConfirm
    ? '请输入环境别名、云账号 ID 或订阅全名...'
    : '请输入您的问题...';

  return (
    <div className="chat-window">
      {/* Thread List Sidebar */}
      <div className={`thread-sidebar ${sidebarOpen ? 'open' : ''}`}>
        <div className="sidebar-header">
          {sidebarOpen && <h3>会话列表</h3>}
          <button 
            className="sidebar-toggle"
            onClick={() => setSidebarOpen(!sidebarOpen)}
            title={sidebarOpen ? '收起侧边栏' : '展开侧边栏'}
          >
            {sidebarOpen ? '◀' : '▶'}
          </button>
        </div>
        {sidebarOpen && (
          <div className="sidebar-content">
            <button 
              className={`thread-item new-thread ${!threadId ? 'active' : ''}`}
              onClick={handleNewThread}
            >
              <span className="thread-icon">+</span>
              <span className="thread-title">新对话</span>
            </button>
            {threadsLoading ? (
              <div className="thread-loading">加载中...</div>
            ) : threads.length === 0 ? (
              <div className="thread-empty">暂无会话</div>
            ) : (
              <div className="thread-list">
                {threads.map((thread) => (
                  <button
                    key={thread.threadId}
                    className={`thread-item ${thread.threadId === threadId ? 'active' : ''}`}
                    onClick={() => handleThreadSelect(thread.threadId)}
                    title={thread.title || formatThreadTitle(thread)}
                  >
                    <span className="thread-title">{formatThreadTitle(thread)}</span>
                  </button>
                ))}
              </div>
            )}
          </div>
        )}
      </div>

      {/* Main Chat Area */}
      <div className="chat-main">
        <div className="chat-header">
          <div className="header-left">
            <button onClick={handleBack} className="back-button" title="返回员工列表">
              ← 返回
            </button>
            <h2>{employee.displayName || employee.name}</h2>
            {selectedCloudAccountId && (
              <span className="chat-account-badge">{selectedCloudAccountId}</span>
            )}
          </div>
          <div className="header-buttons">
            {threadId && (
              <button 
                onClick={handleShare} 
                className={`share-button ${shareCopied ? 'copied' : ''}`} 
                title={shareCopied ? '已复制' : '分享此对话'}
              >
                {shareCopied ? '✓ 已复制' : '🔗 分享'}
              </button>
            )}
          </div>
        </div>

      <div className="messages-container" ref={messagesContainerRef} onScroll={handleScroll}>
        {messages.length === 0 && (
          <div className="welcome-message">
            <h3>👋 欢迎使用 {employee.displayName || employee.name}</h3>
            <p>{employee.description || '您可以询问关于此员工的任何问题'}</p>
          </div>
        )}

        {messages.map((message, index) => (
          <Message 
            key={index} 
            role={message.role} 
            content={message.content}
            events={message.events}
            assistantName={employee?.displayName || employee?.name}
          />
        ))}

        {streamingMessage && (
          <Message 
            role="assistant" 
            events={streamingMessage.events}
            isStreaming={true}
            stage={streamingMessage.stage}
            assistantName={employee?.displayName || employee?.name}
          />
        )}

        {loading && !streamingMessage && (
          <div className="loading-indicator">
            <div className="loading-dots">
              <span></span>
              <span></span>
              <span></span>
            </div>
            <p>AI 正在思考...</p>
          </div>
        )}

        <div ref={messagesEndRef} />
      </div>

      {/* Scroll to bottom button - show when not at bottom */}
      {!autoScroll && (
        <button className="scroll-to-bottom-button" onClick={scrollToBottom}>
          ↓ 回到底部
        </button>
      )}

        <div className="input-container">
          {pendingCloudAccountConfirm && (
            <div className="chat-notice chat-notice-confirm">
              <div className="chat-notice-title">需要确认目标环境</div>
              <div className="chat-notice-text">请回复云账号 ID、环境别名或订阅全名后继续执行原问题。</div>
              {cloudAccountOptionsText && (
                <div className="chat-notice-text">可用账号：{cloudAccountOptionsText}</div>
              )}
              <div className="chat-notice-original">原问题：{pendingCloudAccountConfirm.originalMessage}</div>
            </div>
          )}
          {error && !pendingCloudAccountConfirm && (
            <div className="chat-notice chat-notice-error">{error}</div>
          )}
          <MessageInput 
            onSend={handleSendMessage} 
            onStop={handleStopGeneration}
            disabled={loading}
            isGenerating={loading}
            placeholder={messageInputPlaceholder}
          />
        </div>
      </div>
    </div>
  );
};

export default ChatWindow;
