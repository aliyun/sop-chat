/**
 * API Service
 * Handles communication with backend
 */
import axios from 'axios';
import { getToken } from './auth';

// Version marker for debugging
console.log('[API Service] Loaded version with error event support - v1.0.1');

// 自动检测 API 基础 URL
// 1. 优先使用当前页面的 origin（适用于生产环境和开发环境）
// 2. 开发环境：如果设置了环境变量，优先使用环境变量
// 3. 回退到默认值 localhost:8080
const getApiBaseUrl = () => {
  // 优先使用当前页面的 origin（适用于生产环境和开发环境）
  // 这样无论二进制运行在哪个端口，前端都能正确调用 API
  if (typeof window !== 'undefined' && window.location && window.location.origin) {
    // 判断是否为开发环境
    const isDev = import.meta.env.DEV || import.meta.env.MODE === 'development';
    const envApiUrl = import.meta.env.VITE_API_URL;

    // 开发环境：如果设置了环境变量，优先使用环境变量
    // 生产环境：始终使用 window.location.origin
    if (isDev && envApiUrl) {
      return envApiUrl;
    }

    // 生产环境或开发环境未设置环境变量：使用当前页面的 origin
    return window.location.origin;
  }

  // 回退到默认值（仅用于 SSR 场景或 window 不可用时）
  return import.meta.env.VITE_API_URL || 'http://localhost:8080';
};

// 创建一个函数来动态获取 API URL（运行时获取）
const getApiBaseUrlDynamic = () => {
  return getApiBaseUrl();
};

const API_BASE_URL = getApiBaseUrl();

const apiClient = axios.create({
  baseURL: API_BASE_URL,
  headers: {
    'Content-Type': 'application/json',
  },
  timeout: 30000, // 30 second timeout
});

// Public API client (no authentication)
const publicApiClient = axios.create({
  baseURL: API_BASE_URL,
  headers: {
    'Content-Type': 'application/json',
  },
  timeout: 30000, // 30 second timeout
});

// Add request interceptors
apiClient.interceptors.request.use(
  (config) => {
    // 每次请求时重新获取 API URL，确保使用最新的 window.location.origin
    const currentApiUrl = getApiBaseUrlDynamic();
    if (currentApiUrl) {
      config.baseURL = currentApiUrl;
    }

    // Add token to requests automatically
    const token = getToken();
    if (token) {
      config.headers.Authorization = `Bearer ${token}`;
    }

    return config;
  },
  (error) => {
    return Promise.reject(error);
  }
);

// Public API client interceptor (no auth token)
publicApiClient.interceptors.request.use(
  (config) => {
    // 每次请求时重新获取 API URL，确保使用最新的 window.location.origin
    const currentApiUrl = getApiBaseUrlDynamic();
    if (currentApiUrl) {
      config.baseURL = currentApiUrl;
    }
    // No token added for public API
    return config;
  },
  (error) => {
    return Promise.reject(error);
  }
);

// Public API response interceptor (don't redirect on 401)
publicApiClient.interceptors.response.use(
  (response) => response,
  (error) => {
    // Don't redirect to login for public API errors
    // Just return the error as-is
    return Promise.reject(error);
  }
);

// Handle 401 errors (unauthorized)
apiClient.interceptors.response.use(
  (response) => response,
  (error) => {
    if (error.response?.status === 401) {
      // Token expired or invalid, clear auth and redirect to login
      import('./auth').then(({ clearAuth }) => {
        clearAuth();
        // Only redirect if we're not already on login page
        if (window.location.hash !== '#/login') {
          window.location.href = '/#/login';
        }
      });
    }
    return Promise.reject(error);
  }
);

// ==================== Employee APIs ====================

/**
 * List all digital employees
 * @param {string} namePrefix - Optional name prefix filter
 * @returns {Promise} List of employees
 */
export const listEmployees = async (namePrefix = null, cloudAccountId = '') => {
  try {
    const params = {};
    if (namePrefix) {
      params.namePrefix = namePrefix;
    }
    if (cloudAccountId) {
      params.cloudAccountId = cloudAccountId;
    }
    const response = await apiClient.get('/api/employees', { params });
    return response.data.employees;
  } catch (error) {
    throw new Error(error.response?.data?.detail || '获取员工列表失败');
  }
};

/**
 * Create a new digital employee
 * @param {Object} employeeData - Employee configuration
 * @returns {Promise} Creation result
 */
export const createEmployee = async (employeeData) => {
  try {
    const response = await apiClient.post('/api/employees', employeeData);
    return response.data;
  } catch (error) {
    throw new Error(error.response?.data?.detail || '创建 SOP问答助手失败');
  }
};

/**
 * Get detailed info about a specific employee
 * @param {string} employeeName - Employee name
 * @returns {Promise} Employee info
 */
export const getEmployee = async (employeeName, cloudAccountId = '') => {
  try {
    const response = await apiClient.get(`/api/employees/${employeeName}`, {
      params: cloudAccountId ? { cloudAccountId } : undefined,
    });
    return response.data;
  } catch (error) {
    throw new Error(error.response?.data?.detail || '获取员工信息失败');
  }
};

/**
 * Update an existing digital employee
 * @param {string} employeeName - Employee name
 * @param {Object} employeeData - Employee configuration (without name)
 * @returns {Promise} Update result
 */
export const updateEmployee = async (employeeName, employeeData, cloudAccountId = '') => {
  try {
    const response = await apiClient.put(`/api/employees/${employeeName}`, employeeData, {
      params: cloudAccountId ? { cloudAccountId } : undefined,
    });
    return response.data;
  } catch (error) {
    throw new Error(error.response?.data?.detail || '更新员工失败');
  }
};

// ==================== Thread APIs ====================

/**
 * Create a new thread
 * @param {string} employeeName - Employee name
 * @param {string} title - Thread title
 * @param {Object} attributes - Thread attributes
 * @returns {Promise} Created thread info
 */
export const createThread = async (employeeName, title = '', attributes = {}, cloudAccountId = '') => {
  try {
    const response = await apiClient.post('/api/threads', {
      employeeName,
      cloudAccountId,
      title,
      attributes,
    });
    return response.data;
  } catch (error) {
    throw new Error(error.response?.data?.detail || '创建会话失败');
  }
};

/**
 * List threads for an employee
 * @param {string} employeeName - Employee name
 * @returns {Promise} List of threads
 */
export const listThreads = async (employeeName, cloudAccountId = '') => {
  try {
    const response = await apiClient.get(`/api/threads/${employeeName}`, {
      params: cloudAccountId ? { cloudAccountId } : undefined,
    });
    return response.data.threads;
  } catch (error) {
    throw new Error(error.response?.data?.detail || '获取会话列表失败');
  }
};

/**
 * Get thread details
 * @param {string} employeeName - Employee name
 * @param {string} threadId - Thread ID
 * @returns {Promise} Thread details
 */
export const getThread = async (employeeName, threadId, cloudAccountId = '') => {
  try {
    const response = await apiClient.get(`/api/threads/${employeeName}/${threadId}`, {
      params: cloudAccountId ? { cloudAccountId } : undefined,
    });
    return response.data;
  } catch (error) {
    throw new Error(error.response?.data?.detail || '获取会话信息失败');
  }
};

/**
 * Get thread messages
 * @param {string} employeeName - Employee name
 * @param {string} threadId - Thread ID
 * @returns {Promise} Thread messages
 */
export const getThreadMessages = async (employeeName, threadId, cloudAccountId = '') => {
  try {
    const response = await apiClient.get(`/api/threads/${employeeName}/${threadId}/messages`, {
      params: cloudAccountId ? { cloudAccountId } : undefined,
    });
    return response.data.messages;
  } catch (error) {
    throw new Error(error.response?.data?.detail || '获取消息列表失败');
  }
};

// ==================== Shared Conversation APIs (Public, No Auth Required) ====================

/**
 * Get shared thread (read-only, public access)
 * @param {string} employeeName - Employee name
 * @param {string} threadId - Thread ID
 * @returns {Promise} Thread details
 */
export const getSharedThread = async (employeeName, threadId, cloudAccountId = '') => {
  try {
    const response = await publicApiClient.get(
      `/api/share/${encodeURIComponent(employeeName)}/${encodeURIComponent(threadId)}`,
      { params: cloudAccountId ? { cloudAccountId } : undefined }
    );
    return response.data;
  } catch (error) {
    throw new Error(error.response?.data?.detail || '获取会话信息失败');
  }
};

/**
 * Get shared thread messages (read-only, public access)
 * @param {string} employeeName - Employee name
 * @param {string} threadId - Thread ID
 * @returns {Promise} Thread messages
 */
export const getSharedThreadMessages = async (employeeName, threadId, cloudAccountId = '') => {
  try {
    const response = await publicApiClient.get(
      `/api/share/${encodeURIComponent(employeeName)}/${encodeURIComponent(threadId)}/messages`,
      { params: cloudAccountId ? { cloudAccountId } : undefined }
    );
    return response.data.messages;
  } catch (error) {
    throw new Error(error.response?.data?.detail || '获取消息列表失败');
  }
};

/**
 * Get shared employee info (read-only, public access)
 * @param {string} employeeName - Employee name
 * @returns {Promise} Employee info
 */
export const getSharedEmployee = async (employeeName, cloudAccountId = '') => {
  try {
    const response = await publicApiClient.get(`/api/share/employee/${encodeURIComponent(employeeName)}`, {
      params: cloudAccountId ? { cloudAccountId } : undefined,
    });
    return response.data;
  } catch (error) {
    throw new Error(error.response?.data?.detail || '获取员工信息失败');
  }
};

// ==================== Chat APIs ====================

/**
 * Send a chat message with SSE streaming
 * @param {string} employeeName - Employee name
 * @param {string} threadId - Thread ID (optional for new threads)
 * @param {string} message - User's message
 * @param {Function} onMeta - Callback for meta info
 * @param {Function} onChunk - Callback for each content chunk
 * @param {Function} onToolCall - Callback when a tool is called
 * @param {Function} onToolResult - Callback when a tool returns result
 * @param {Function} onErrorEvent - Callback for error events in stream
 * @param {Function} onComplete - Callback on completion
 * @param {Function} onError - Callback on error
 * @param {AbortSignal} signal - Optional abort signal for cancellation
 */
export const sendChatMessageStream = async (
  employeeName,
  threadId,
  message,
  cloudAccountId,
  onMeta,
  onChunk,
  onToolCall,
  onToolResult,
  onErrorEvent,
  onComplete,
  onError,
  signal
) => {
  try {
    const requestBody = {
      employeeName,
      message,
    };
    if (threadId) {
      requestBody.threadId = threadId;
    }
    if (cloudAccountId) {
      requestBody.cloudAccountId = cloudAccountId;
    }

    // Get token for SSE request
    const token = getToken();
    const headers = {
      'Content-Type': 'application/json',
    };
    if (token) {
      headers.Authorization = `Bearer ${token}`;
    }

    // 动态获取 API URL，确保使用当前页面的 origin
    const apiUrl = getApiBaseUrlDynamic();
    const response = await fetch(`${apiUrl}/api/chat/stream`, {
      method: 'POST',
      headers,
      body: JSON.stringify(requestBody),
      signal, // Add abort signal support
    });

    if (!response.ok) {
      // Try to extract error detail from response
      let errorMessage = `HTTP ${response.status} 错误`;
      let errorData = null;
      try {
        errorData = await response.json();
        if (errorData.detail) {
          errorMessage = errorData.detail;
        } else if (errorData.error) {
          errorMessage = errorData.error;
        }
      } catch (parseError) {
        // Ignore parse error, use default message
      }

      const error = new Error(errorMessage);
      if (errorData && typeof errorData === 'object') {
        error.needConfirm = errorData.needConfirm === true;
        error.options = Array.isArray(errorData.options) ? errorData.options : [];
        error.detail = typeof errorData.detail === 'string' ? errorData.detail : '';
      }
      throw error;
    }

    const reader = response.body.getReader();
    const decoder = new TextDecoder();

    let buffer = '';
    // 用于累积同一 callId 的文本内容
    const contentBuffers = new Map();

    while (true) {
      const { done, value } = await reader.read();

      if (done) {
        break;
      }

      // Decode the chunk
      const chunk = decoder.decode(value, { stream: true });
      buffer += chunk;

      // Split by lines (SSE format)
      const lines = buffer.split('\n');

      // Keep the last incomplete line in the buffer
      buffer = lines.pop() || '';

      for (const line of lines) {
        if (line.startsWith('data: ')) {
          try {
            const jsonStr = line.slice(6);
            const msg = JSON.parse(jsonStr);

            // 处理完成消息
            if (msg.type === 'done') {
              if (msg.threadId) {
                onMeta && onMeta(msg.threadId);
              }
              onComplete && onComplete();
              continue;
            }

            // 处理错误消息
            if (msg.type === 'error') {
              let errorMessage = '发生错误';
              if (typeof msg.error === 'string') {
                errorMessage = msg.error;
              } else if (msg.error && typeof msg.error === 'object') {
                if (msg.error.message) {
                  errorMessage = msg.error.message;
                } else if (msg.error.code) {
                  errorMessage = `${msg.error.code}: ${msg.error.message || '未知错误'}`;
                }
              }
              onError && onError(new Error(errorMessage));
              continue;
            }

            // 处理文本内容 (contents 数组)
            if (msg.contents && Array.isArray(msg.contents) && msg.role === 'assistant') {
              const callId = msg.callId || 'default';
              let contentBuffer = contentBuffers.get(callId) || '';

              for (const content of msg.contents) {
                if (content.type === 'text' && content.value !== undefined) {
                  if (content.append !== false) {
                    // 追加模式（默认）：累积内容
                    contentBuffer += content.value;
                  } else {
                    // 非追加模式：替换内容
                    contentBuffer = content.value;
                  }

                  // 更新缓冲区
                  contentBuffers.set(callId, contentBuffer);

                  // 实时发送累积的内容（用于流式显示）
                  if (contentBuffer) {
                    onChunk && onChunk(contentBuffer);
                  }

                  // 如果是最后一个 chunk，清空缓冲区
                  if (content.lastChunk) {
                    contentBuffers.delete(callId);
                  }
                } else if (content.type === 'spin_text') {
                  // 处理加载提示文本（如"稍等片刻..."）
                  // 可以忽略或显示为加载状态
                }
              }
            }

            // 处理工具调用 (tools 数组)
            if (msg.tools && Array.isArray(msg.tools)) {
              for (const tool of msg.tools) {
                const toolName = tool.name || '';
                const status = tool.status || '';
                const arguments_ = tool.arguments || {};

                if (status === 'start') {
                  // 工具调用开始
                  onToolCall && onToolCall(toolName, arguments_, 'start');
                } else if (status === 'success' || status === 'completed') {
                  // 工具调用成功
                  onToolCall && onToolCall(toolName, arguments_, 'success');

                  // 如果有 contents，发送工具结果
                  if (tool.contents && Array.isArray(tool.contents)) {
                    const result = {
                      success: true,
                      contents: tool.contents
                    };
                    onToolResult && onToolResult(toolName, result);
                  }
                } else if (status === 'fail') {
                  // 工具调用失败
                  onToolCall && onToolCall(toolName, arguments_, 'fail');

                  // 如果有 contents，发送错误结果
                  if (tool.contents && Array.isArray(tool.contents)) {
                    const result = {
                      success: false,
                      contents: tool.contents
                    };
                    onToolResult && onToolResult(toolName, result);
                  } else {
                    // 没有 contents，发送默认错误
                    const result = {
                      success: false,
                      error: '工具调用失败'
                    };
                    onToolResult && onToolResult(toolName, result);
                  }
                }
              }
            }

            // 处理事件 (events 数组)
            if (msg.events && Array.isArray(msg.events)) {
              for (const event of msg.events) {
                if (event.type === 'error' && event.payload) {
                  // 错误事件 - 作为消息的一部分显示，不中断流程
                  console.log('[SSE] Error event received:', event.payload);
                  onErrorEvent && onErrorEvent(event.payload);
                } else if (event.type === 'thread_title_updated' && event.payload && event.payload.title) {
                  // 线程标题更新事件，可以在这里处理
                  // 目前不需要特殊处理，因为前端不显示标题
                } else if (event.type === 'task_finished' && event.payload) {
                  // 任务完成事件
                  // 可以在这里处理，但目前不需要特殊操作
                }
              }
            }

          } catch (e) {
            console.error('Error parsing SSE data:', e, line);
          }
        }
      }
    }
  } catch (error) {
    // Check if the error is due to abort
    if (error.name === 'AbortError') {
      onError && onError(new Error('请求已被用户取消'));
    } else {
      console.error('Request failed:', error);
      onError && onError(error);
    }
  }
};

// ==================== System APIs ====================

/**
 * Get current account ID
 * @returns {Promise} Account ID
 */
export const getAccountId = async () => {
  try {
    const response = await apiClient.get('/api/system/account-id');
    return response.data.accountId;
  } catch (error) {
    throw new Error(error.response?.data?.detail || '获取账号ID失败');
  }
};

// ==================== Health Check ====================

/**
 * Check backend health
 * @returns {Promise} Health status
 */
export const checkHealth = async () => {
  try {
    const response = await apiClient.get('/api/health');
    return response.data;
  } catch (error) {
    throw new Error('健康检查失败');
  }
};

// ==================== Feedback ====================

/**
 * Submit user feedback (like/dislike/copy) for an AI response
 * @param {string} conversationId - Conversation ID (not used in our simple version)
 * @param {string} requestId - Request ID (not used in our simple version)
 * @param {string} feedbackType - "like", "dislike", or "copy"
 * @param {string} reason - Optional reason for dislike
 * @param {string} question - Original user question
 * @param {string} answer - AI's answer
 * @returns {Promise} Feedback result
 */
export const submitFeedback = async (conversationId, requestId, feedbackType, reason = null, question = null, answer = null) => {
  try {
    // For now, just return success since we don't have a feedback endpoint
    // You can implement this later if needed
    return { success: true };
  } catch (error) {
    throw new Error('提交反馈失败');
  }
};

// 默认导出 apiClient，用于在其他组件中直接使用
export default apiClient;
