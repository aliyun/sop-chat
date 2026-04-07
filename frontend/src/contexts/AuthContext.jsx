/**
 * Authentication Context
 * Provides authentication state and methods throughout the app
 */
import { createContext, useContext, useState, useEffect } from 'react';
import { getToken, getUser, getCurrentUser, clearAuth, setToken as setStoredToken, setUser as setStoredUser, login as loginService, logout as logoutService } from '../services/auth';

const AuthContext = createContext(null);

export const useAuth = () => {
  const context = useContext(AuthContext);
  if (!context) {
    throw new Error('useAuth must be used within an AuthProvider');
  }
  return context;
};

export const AuthProvider = ({ children }) => {
  const [user, setUser] = useState(null);
  const [token, setToken] = useState(null);
  const [loading, setLoading] = useState(true);
  const [isAuthenticated, setIsAuthenticated] = useState(false);

  // Initialize auth state from localStorage
  useEffect(() => {
    const initAuth = async () => {
      try {
        const storedToken = getToken();
        const storedUser = getUser();
        
        if (storedToken && storedUser) {
          setToken(storedToken);
          setUser(storedUser);
          setIsAuthenticated(true);
          
          // Optionally verify token with server
          try {
            const currentUser = await getCurrentUser();
            setUser(currentUser);
          } catch (error) {
            // Token invalid, clear auth
            clearAuth();
            setToken(null);
            setUser(null);
            setIsAuthenticated(false);
          }
        }
      } catch (error) {
        console.error('Auth initialization error:', error);
        clearAuth();
      } finally {
        setLoading(false);
      }
    };
    
    initAuth();
  }, []);

  const login = async (username, password) => {
    const result = await loginService(username, password);
    setToken(result.token);
    setUser(result.user);
    setIsAuthenticated(true);
    return result;
  };

  // OIDC 回调登录：直接使用 token 和 user 信息完成登录
  const loginWithToken = (tokenStr, userObj) => {
    setStoredToken(tokenStr);
    setStoredUser(userObj);
    setToken(tokenStr);
    setUser(userObj);
    setIsAuthenticated(true);
    setLoading(false);
  };

  const logout = async () => {
    await logoutService();
    setToken(null);
    setUser(null);
    setIsAuthenticated(false);
  };

  const value = {
    user,
    token,
    isAuthenticated,
    loading,
    login,
    loginWithToken,
    logout,
  };

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
};
