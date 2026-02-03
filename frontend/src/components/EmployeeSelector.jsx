/**
 * EmployeeSelector Component
 * Allows user to select a digital employee
 */
import React, { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { listEmployees } from '../services/api';
import { useAuth } from '../contexts/AuthContext';

const EmployeeSelector = () => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { user } = useAuth();
  const [employees, setEmployees] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  
  // 检查用户是否有 admin 角色
  // 注意：后端返回的字段是大写开头的 (Roles)，也要兼容小写 (roles)
  const userRoles = user?.Roles || user?.roles || [];
  const isAdmin = userRoles.includes('admin');
  
  // 调试日志
  console.log('[EmployeeSelector] User:', user);
  console.log('[EmployeeSelector] User roles:', userRoles);
  console.log('[EmployeeSelector] isAdmin:', isAdmin);

  // 图标组件数组
  const iconComponents = [
    // 对话气泡
    (gradId) => (
      <svg viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
        <defs>
          <linearGradient id={gradId} x1="0%" y1="0%" x2="100%" y2="100%">
            <stop offset="0%" style={{stopColor: '#ffffff', stopOpacity: 0.95}} />
            <stop offset="100%" style={{stopColor: '#ffffff', stopOpacity: 0.85}} />
          </linearGradient>
        </defs>
        <path d="M20 2H4c-1.1 0-2 .9-2 2v18l4-4h14c1.1 0 2-.9 2-2V4c0-1.1-.9-2-2-2zm-3 12H7c-.55 0-1-.45-1-1s.45-1 1-1h10c.55 0 1 .45 1 1s-.45 1-1 1zm0-3H7c-.55 0-1-.45-1-1s.45-1 1-1h10c.55 0 1 .45 1 1s-.45 1-1 1zm0-3H7c-.55 0-1-.45-1-1s.45-1 1-1h10c.55 0 1 .45 1 1s-.45 1-1 1z" 
          fill={`url(#${gradId})`} 
          stroke="rgba(255,255,255,0.3)" 
          strokeWidth="0.5" />
      </svg>
    ),
    // 闪电
    (gradId) => (
      <svg viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
        <defs>
          <linearGradient id={gradId} x1="0%" y1="0%" x2="100%" y2="100%">
            <stop offset="0%" style={{stopColor: '#ffffff', stopOpacity: 0.95}} />
            <stop offset="100%" style={{stopColor: '#ffffff', stopOpacity: 0.85}} />
          </linearGradient>
        </defs>
        <path d="M7 2v11h3v9l7-12h-4l4-8z" 
          fill={`url(#${gradId})`} 
          stroke="rgba(255,255,255,0.3)" 
          strokeWidth="0.5" />
      </svg>
    ),
    // 书籍
    (gradId) => (
      <svg viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
        <defs>
          <linearGradient id={gradId} x1="0%" y1="0%" x2="100%" y2="100%">
            <stop offset="0%" style={{stopColor: '#ffffff', stopOpacity: 0.95}} />
            <stop offset="100%" style={{stopColor: '#ffffff', stopOpacity: 0.85}} />
          </linearGradient>
        </defs>
        <path d="M18 2H6c-1.1 0-2 .9-2 2v16c0 1.1.9 2 2 2h12c1.1 0 2-.9 2-2V4c0-1.1-.9-2-2-2zM6 4h5v8l-2.5-1.5L6 12V4z" 
          fill={`url(#${gradId})`} 
          stroke="rgba(255,255,255,0.3)" 
          strokeWidth="0.5" />
      </svg>
    ),
    // 灯泡（智能）
    (gradId) => (
      <svg viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
        <defs>
          <linearGradient id={gradId} x1="0%" y1="0%" x2="100%" y2="100%">
            <stop offset="0%" style={{stopColor: '#ffffff', stopOpacity: 0.95}} />
            <stop offset="100%" style={{stopColor: '#ffffff', stopOpacity: 0.85}} />
          </linearGradient>
        </defs>
        <path d="M9 21c0 .55.45 1 1 1h4c.55 0 1-.45 1-1v-1H9v1zm3-19C8.14 2 5 5.14 5 9c0 2.38 1.19 4.47 3 5.74V17c0 .55.45 1 1 1h6c.55 0 1-.45 1-1v-2.26c1.81-1.27 3-3.36 3-5.74 0-3.86-3.14-7-7-7z" 
          fill={`url(#${gradId})`} 
          stroke="rgba(255,255,255,0.3)" 
          strokeWidth="0.5" />
      </svg>
    ),
    // 星星（特色）
    (gradId) => (
      <svg viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
        <defs>
          <linearGradient id={gradId} x1="0%" y1="0%" x2="100%" y2="100%">
            <stop offset="0%" style={{stopColor: '#ffffff', stopOpacity: 0.95}} />
            <stop offset="100%" style={{stopColor: '#ffffff', stopOpacity: 0.85}} />
          </linearGradient>
        </defs>
        <path d="M12 17.27L18.18 21l-1.64-7.03L22 9.24l-7.19-.61L12 2 9.19 8.63 2 9.24l5.46 4.73L5.82 21z" 
          fill={`url(#${gradId})`} 
          stroke="rgba(255,255,255,0.3)" 
          strokeWidth="0.5" />
      </svg>
    ),
    // 齿轮（工具）
    (gradId) => (
      <svg viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
        <defs>
          <linearGradient id={gradId} x1="0%" y1="0%" x2="100%" y2="100%">
            <stop offset="0%" style={{stopColor: '#ffffff', stopOpacity: 0.95}} />
            <stop offset="100%" style={{stopColor: '#ffffff', stopOpacity: 0.85}} />
          </linearGradient>
        </defs>
        <path d="M19.14 12.94c.04-.3.06-.61.06-.94 0-.32-.02-.64-.07-.94l2.03-1.58c.18-.14.23-.41.12-.61l-1.92-3.32c-.12-.22-.37-.29-.59-.22l-2.39.96c-.5-.38-1.03-.7-1.62-.94l-.36-2.54c-.04-.24-.24-.41-.48-.41h-3.84c-.24 0-.43.17-.47.41l-.36 2.54c-.59.24-1.13.57-1.62.94l-2.39-.96c-.22-.08-.47 0-.59.22L2.74 8.87c-.12.21-.08.47.12.61l2.03 1.58c-.05.3-.09.63-.09.94s.02.64.07.94l-2.03 1.58c-.18.14-.23.41-.12.61l1.92 3.32c.12.22.37.29.59.22l2.39-.96c.5.38 1.03.7 1.62.94l.36 2.54c.05.24.24.41.48.41h3.84c.24 0 .44-.17.47-.41l.36-2.54c.59-.24 1.13-.56 1.62-.94l2.39.96c.22.08.47 0 .59-.22l1.92-3.32c.12-.22.07-.47-.12-.61l-2.01-1.58zM12 15.6c-1.98 0-3.6-1.62-3.6-3.6s1.62-3.6 3.6-3.6 3.6 1.62 3.6 3.6-1.62 3.6-3.6 3.6z" 
          fill={`url(#${gradId})`} 
          stroke="rgba(255,255,255,0.3)" 
          strokeWidth="0.5" />
      </svg>
    ),
    // 火箭（快速）
    (gradId) => (
      <svg viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
        <defs>
          <linearGradient id={gradId} x1="0%" y1="0%" x2="100%" y2="100%">
            <stop offset="0%" style={{stopColor: '#ffffff', stopOpacity: 0.95}} />
            <stop offset="100%" style={{stopColor: '#ffffff', stopOpacity: 0.85}} />
          </linearGradient>
        </defs>
        <path d="M9.19 6.35c-2.04 2.29-3.44 5.58-3.57 5.89L2 13.12l8.23 8.23.88-3.62c.31-.13 3.6-1.53 5.89-3.57C19.94 11.21 22 4.5 22 4.5S15.29 6.56 12.35 9.5c-.95.95-1.64 2.06-2.11 3.17-.39-1.05-1.08-2.16-2.05-3.13-.97-.97-2.08-1.66-3.13-2.05 1.11-.47 2.22-1.16 3.17-2.11C11.17 2.44 17.88.38 17.88.38S15.82 7.09 12.88 10.03c-.95.95-2.06 1.64-3.17 2.11.39-1.05 1.08-2.16 2.05-3.13.97-.97 2.08-1.66 3.13-2.05-1.11.47-2.22 1.16-3.17 2.11z" 
          fill={`url(#${gradId})`} 
          stroke="rgba(255,255,255,0.3)" 
          strokeWidth="0.5" />
      </svg>
    ),
    // 心形（关怀）
    (gradId) => (
      <svg viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
        <defs>
          <linearGradient id={gradId} x1="0%" y1="0%" x2="100%" y2="100%">
            <stop offset="0%" style={{stopColor: '#ffffff', stopOpacity: 0.95}} />
            <stop offset="100%" style={{stopColor: '#ffffff', stopOpacity: 0.85}} />
          </linearGradient>
        </defs>
        <path d="M12 21.35l-1.45-1.32C5.4 15.36 2 12.28 2 8.5 2 5.42 4.42 3 7.5 3c1.74 0 3.41.81 4.5 2.09C13.09 3.81 14.76 3 16.5 3 19.58 3 22 5.42 22 8.5c0 3.78-3.4 6.86-8.55 11.54L12 21.35z" 
          fill={`url(#${gradId})`} 
          stroke="rgba(255,255,255,0.3)" 
          strokeWidth="0.5" />
      </svg>
    )
  ];

  // 根据索引获取图标
  const getIcon = (index, employeeName) => {
    const iconIndex = index % iconComponents.length;
    const gradId = `grad-${employeeName}-${iconIndex}`;
    return iconComponents[iconIndex](gradId);
  };

  useEffect(() => {
    fetchEmployees();
  }, []);

  const fetchEmployees = async () => {
    try {
      setLoading(true);
      const data = await listEmployees();
      setEmployees(data);
      setError(null);
    } catch (err) {
      console.error('Failed to fetch employees:', err);
      setError(err.message);
    } finally {
      setLoading(false);
    }
  };

  const handleSelect = (employee) => {
    navigate(`/chat/${employee.name}`);
  };

  const handleSettingsClick = (e, employee) => {
    e.stopPropagation(); // 阻止触发卡片点击
    navigate(`/settings/${employee.name}`);
  };

  const handleCreateEmployee = () => {
    navigate('/create-employee');
  };

  if (loading) {
    return (
      <div className="app-list-container">
        <div className="app-list-loading">
          <div className="loading-spinner"></div>
          <p>{t('employeeSelector.loading')}</p>
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="app-list-container">
        <div className="app-list-error">
          <div className="error-icon">⚠️</div>
          <p>{error}</p>
          <button className="retry-button" onClick={() => window.location.reload()}>
            {t('common.retry', { defaultValue: 'Retry' })}
          </button>
        </div>
      </div>
    );
  }

  if (employees.length === 0) {
    return (
      <div className="app-list-container">
        <div className="app-list-empty">
          <div className="empty-icon">📭</div>
          <h3>{t('employeeSelector.noEmployees')}</h3>
          <p>{isAdmin ? t('employeeSelector.createFirst') : t('employeeSelector.noEmployees')}</p>
          {isAdmin && (
            <button className="create-app-btn" onClick={handleCreateEmployee}>
              ➕ {t('nav.createEmployee')}
            </button>
          )}
        </div>
      </div>
    );
  }

  return (
    <div className="app-list-container">
      <div className="app-list-header">
        <div className="header-title">
          <h1>{t('employeeSelector.title')}</h1>
          <p className="subtitle">{t('employeeSelector.selectToStart', { defaultValue: 'Select an assistant to start conversation' })}</p>
        </div>
        {isAdmin && (
          <button className="create-app-btn" onClick={handleCreateEmployee}>
            ➕ {t('nav.createEmployee')}
          </button>
        )}
      </div>

      <div className="app-grid">
        {employees.map((employee, index) => (
          <div
            key={employee.name}
            className="app-card"
            onClick={() => handleSelect(employee)}
          >
            <div className="app-card-icon">
              {getIcon(index, employee.name)}
            </div>
            <div className="app-card-content">
              <div className="app-card-title-row">
                <h3 className="app-card-title">{employee.displayName || employee.name}</h3>
                {isAdmin && (
                  <button
                    className="app-settings-btn"
                    onClick={(e) => handleSettingsClick(e, employee)}
                    title={t('nav.settings')}
                  >
                    ⚙️
                  </button>
                )}
              </div>
              {employee.description && (
                <p className="app-card-description">{employee.description}</p>
              )}
            </div>
            <div className="app-card-arrow">→</div>
          </div>
        ))}
      </div>
    </div>
  );
};

export default EmployeeSelector;
