/**
 * Main App Component with React Router
 */
import { useEffect } from 'react';
import { HashRouter as Router, Routes, Route, Navigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { AuthProvider } from './contexts/AuthContext';
import ProtectedRoute from './components/ProtectedRoute';
import Layout from './components/Layout';
import Login from './components/Login';
import EmployeeSelector from './components/EmployeeSelector';
import ChatWindow from './components/ChatWindow';
import EmployeeSettings from './components/EmployeeSettings';
import ShareChat from './components/ShareChat';
import api from './services/api';
import './i18n'; // 导入 i18n 配置
import './App.css';

function App() {
  const { i18n } = useTranslation();

  useEffect(() => {
    // 从后端获取系统配置（语言设置）
    api.get('/api/system/config')
      .then(response => {
        const { language } = response.data;
        if (language && i18n.language !== language) {
          i18n.changeLanguage(language);
        }
      })
      .catch(error => {
        console.error('Failed to load system config:', error);
        // 失败时使用默认语言
      });
  }, [i18n]);

  return (
    <AuthProvider>
      <Router>
        <div className="app">
          <Routes>
            {/* Public routes */}
            <Route path="/login" element={<Login />} />
            
            {/* Share route (public, no authentication required) */}
            <Route
              path="/share/:employeeName/:threadId"
              element={
                <div className="app-content">
                  <ShareChat />
                </div>
              }
            />
            
            {/* Protected routes with layout */}
            <Route
              path="/"
              element={
                <ProtectedRoute>
                  <Layout>
                    <EmployeeSelector />
                  </Layout>
                </ProtectedRoute>
              }
            />
            <Route
              path="/chat/:employeeId"
              element={
                <ProtectedRoute>
                  <Layout>
                    <ChatWindow />
                  </Layout>
                </ProtectedRoute>
              }
            />
            <Route
              path="/settings/:employeeId"
              element={
                <ProtectedRoute>
                  <Layout>
                    <EmployeeSettings />
                  </Layout>
                </ProtectedRoute>
              }
            />
            
            {/* Default redirect */}
            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        </div>
      </Router>
    </AuthProvider>
  );
}

export default App;
