/**
 * Login Component
 */
import { useState, useEffect } from 'react';
import { useNavigate, useLocation } from 'react-router-dom';
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
  const { login, loginWithToken, isAuthenticated } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();

  // 处理 OIDC 回调：从 URL 参数中提取 token 和 user
  useEffect(() => {
    const params = new URLSearchParams(location.search);
    const token = params.get('token');
    const userStr = params.get('user');

    if (token && userStr) {
      try {
        const user = JSON.parse(decodeURIComponent(userStr));
        loginWithToken(token, user);
        // 不在这里 navigate，由下方 isAuthenticated 的 useEffect 统一处理跳转
      } catch (e) {
        setError(t('login.loginFailed'));
      }
    }
  }, [location.search, loginWithToken, navigate, t]);

  // 如果已登录，直接跳转
  useEffect(() => {
    if (isAuthenticated) {
      navigate('/', { replace: true });
    }
  }, [isAuthenticated, navigate]);

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

  const handleOIDCLogin = () => {
    setError('');
    setLoading(true);
    // 直接跳转，由后端 302 到 OIDC Provider
    window.location.href = '/api/auth/oidc/login';
  };

  // 状态尚未加载完成时渲染空白，避免登录表单闪烁后被替换
  if (setupStatus === null) {
    return <div className="login-container" />;
  }

  const notConfigured = !setupStatus.configured;
  const authMethods = setupStatus.authMethods || [];
  const hasBuiltin = authMethods.includes('builtin');
  const hasOIDC = authMethods.includes('oidc');
  const oidcDisplayName = setupStatus.oidcDisplayName;

  if (notConfigured && !hasOIDC) {
    const reasons = [];
    if (!setupStatus.credConfigured) reasons.push(t('login.akNotConfigured'));
    if (!setupStatus.authConfigured) reasons.push(t('login.authNotConfigured'));
    if (!setupStatus.usersConfigured) reasons.push(t('login.usersNotConfigured'));

    return (
      <div className="login-container">
        <div className="setup-required-box">
          <div className="setup-required-icon">⚙️</div>
          <h2 className="setup-required-title">{t('login.cannotLogin')}</h2>
          <ul className="setup-required-reasons">
            {reasons.map((r, i) => <li key={i}>{r}</li>)}
          </ul>
          <p className="setup-required-hint">
            {t('login.setupHint')}
          </p>
          <p className="setup-required-path">
            {t('login.setupUrlFormat')}<code>http://&lt;host&gt;:&lt;port&gt;/admin-ui?token=...</code>
          </p>
        </div>
      </div>
    );
  }

  return (
    <div className="login-container">
      <div className="login-box">
        <h1 className="login-title">{t('login.title')}</h1>

        {error && <div className="error-message">{error}</div>}

        {hasBuiltin && (
          <form onSubmit={handleSubmit} className="login-form">
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
        )}

        {hasOIDC && hasBuiltin && (
          <div className="login-divider">
            <span>{t('login.or')}</span>
          </div>
        )}

        {hasOIDC && (
          <button
            type="button"
            className="login-button oidc-login-button"
            onClick={handleOIDCLogin}
          >
            {oidcDisplayName}
          </button>
        )}
      </div>
    </div>
  );
}

export default Login;
