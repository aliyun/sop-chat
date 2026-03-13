/**
 * Login Component
 */
import { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { useAuth } from '../contexts/AuthContext';
import './Login.css';

function Login() {
  const { t } = useTranslation();
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const [setupStatus, setSetupStatus] = useState(null);
  const { login } = useAuth();
  const navigate = useNavigate();

  useEffect(() => {
    fetch('/api/system/setup-status')
      .then(r => r.ok ? r.json() : null)
      .then(data => { if (data) setSetupStatus(data); })
      .catch(() => {});
  }, []);

  const handleSubmit = async (e) => {
    e.preventDefault();
    setError('');
    setLoading(true);

    try {
      await login(username, password);
      navigate('/');
    } catch (err) {
      setError(err.message || t('login.loginFailed'));
    } finally {
      setLoading(false);
    }
  };

  // 状态尚未加载完成时渲染空白，避免登录表单闪烁后被替换
  if (setupStatus === null) {
    return <div className="login-container" />;
  }

  const notConfigured = !setupStatus.configured;

  if (notConfigured) {
    const reasons = [];
    if (!setupStatus.credConfigured) reasons.push('阿里云 AccessKey 未填写');
    if (!setupStatus.authConfigured) reasons.push('登录认证方式（auth.methods）未配置');
    if (!setupStatus.usersConfigured) reasons.push('尚未创建任何登录用户');

    return (
      <div className="login-container">
        <div className="setup-required-box">
          <div className="setup-required-icon">⚙️</div>
          <h2 className="setup-required-title">暂时无法登录</h2>
          <ul className="setup-required-reasons">
            {reasons.map((r, i) => <li key={i}>{r}</li>)}
          </ul>
          <p className="setup-required-hint">
            请在命令终端中找到配置管理 UI 地址（服务启动时已打印），在浏览器中打开后完成相关配置。
          </p>
          <p className="setup-required-path">
            形如：<code>http://&lt;host&gt;:&lt;port&gt;/admin-ui?token=...</code>
          </p>
        </div>
      </div>
    );
  }

  return (
    <div className="login-container">
      <div className="login-box">
        <h1 className="login-title">{t('login.title')}</h1>
        <form onSubmit={handleSubmit} className="login-form">
          {error && <div className="error-message">{error}</div>}

          <div className="form-group">
            <label htmlFor="username">{t('login.username')}</label>
            <input
              id="username"
              type="text"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              required
              autoFocus
              disabled={loading}
            />
          </div>

          <div className="form-group">
            <label htmlFor="password">{t('login.password')}</label>
            <input
              id="password"
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
              disabled={loading}
            />
          </div>

          <button type="submit" className="login-button" disabled={loading}>
            {loading ? t('login.loggingIn') : t('login.loginButton')}
          </button>
        </form>
      </div>
    </div>
  );
}

export default Login;
