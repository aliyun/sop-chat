/**
 * Authentication Service
 * Handles authentication, token management, and user session
 */
import axios from 'axios';

// 自动检测 API 基础 URL（与 api.js 使用相同的逻辑）
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

const authClient = axios.create({
  baseURL: API_BASE_URL,
  headers: {
    'Content-Type': 'application/json',
  },
  timeout: 30000,
});

// 在请求拦截器中动态更新 baseURL（确保使用最新的 window.location）
authClient.interceptors.request.use(
  (config) => {
    // 每次请求时重新获取 API URL，确保使用最新的 window.location.origin
    const currentApiUrl = getApiBaseUrlDynamic();
    if (currentApiUrl) {
      config.baseURL = currentApiUrl;
    }
    return config;
  },
  (error) => {
    return Promise.reject(error);
  }
);

// Token storage key
const TOKEN_KEY = 'auth_token';
const USER_KEY = 'auth_user';

/**
 * Get stored token
 */
export const getToken = () => {
  return localStorage.getItem(TOKEN_KEY);
};

/**
 * Set token in storage
 */
export const setToken = (token) => {
  if (token) {
    localStorage.setItem(TOKEN_KEY, token);
  } else {
    localStorage.removeItem(TOKEN_KEY);
  }
};

/**
 * Get stored user info
 */
export const getUser = () => {
  const userStr = localStorage.getItem(USER_KEY);
  if (userStr) {
    try {
      return JSON.parse(userStr);
    } catch (e) {
      return null;
    }
  }
  return null;
};

/**
 * Set user info in storage
 */
export const setUser = (user) => {
  if (user) {
    localStorage.setItem(USER_KEY, JSON.stringify(user));
  } else {
    localStorage.removeItem(USER_KEY);
  }
};

/**
 * Clear authentication data
 */
export const clearAuth = () => {
  localStorage.removeItem(TOKEN_KEY);
  localStorage.removeItem(USER_KEY);
};

/**
 * Check if user is authenticated
 */
export const isAuthenticated = () => {
  return !!getToken();
};

/**
 * Login with username and password
 * @param {string} username - Username
 * @param {string} password - Password
 * @returns {Promise} Login response with token and user info
 */
export const login = async (username, password) => {
  try {
    const response = await authClient.post('/api/auth/login', {
      username,
      password,
    });
    
    const { token, user } = response.data;
    
    // Store token and user info
    setToken(token);
    setUser(user);
    
    return { token, user };
  } catch (error) {
    const errorMessage = error.response?.data?.detail || error.response?.data?.error || '登录失败';
    throw new Error(errorMessage);
  }
};

/**
 * Logout current user
 */
export const logout = async () => {
  try {
    // Call logout endpoint if needed
    const token = getToken();
    if (token) {
      await authClient.post('/api/auth/logout', {}, {
        headers: {
          Authorization: `Bearer ${token}`,
        },
      });
    }
  } catch (error) {
    // Ignore logout errors, still clear local storage
    console.error('Logout error:', error);
  } finally {
    // Always clear local storage
    clearAuth();
  }
};

/**
 * Get current user info from server
 * @returns {Promise} Current user info
 */
export const getCurrentUser = async () => {
  try {
    const token = getToken();
    if (!token) {
      throw new Error('未登录');
    }
    
    const response = await authClient.get('/api/auth/me', {
      headers: {
        Authorization: `Bearer ${token}`,
      },
    });
    
    const user = response.data;
    setUser(user);
    
    return user;
  } catch (error) {
    // If unauthorized, clear auth data
    if (error.response?.status === 401) {
      clearAuth();
    }
    throw error;
  }
};
