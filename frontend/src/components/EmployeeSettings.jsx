/**
 * EmployeeSettings Component
 * Full page settings view to display and edit employee configuration with tabs
 */
import React, { useState, useEffect } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { getEmployee, updateEmployee, getAccountId } from '../services/api';
import { useAuth } from '../contexts/AuthContext';
import { useTranslation } from 'react-i18next';

const EmployeeSettings = () => {
  const { employeeId } = useParams();
  const navigate = useNavigate();
  const { user } = useAuth();
  const [activeTab, setActiveTab] = useState('info'); // info, knowledges
  const [config, setConfig] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const [editMode, setEditMode] = useState(false);
  const [saving, setSaving] = useState(false);
  const [accountId, setAccountId] = useState(null);
  
  // 检查用户是否有 admin 角色
  // 注意：后端返回的字段是大写开头的 (Roles)，也要兼容小写 (roles)
  const userRoles = user?.Roles || user?.roles || [];
  const isAdmin = userRoles.includes('admin');
  const { t } = useTranslation();

  // 编辑表单数据
  const [formData, setFormData] = useState({
    displayName: '',
    description: '',
    defaultRule: '',
    roleArn: ''
  });

  // SOP 知识库配置
  const [sopConfigs, setSopConfigs] = useState([]);

  const loadConfig = async () => {
    if (!employeeId) return;
    setLoading(true);
    setError(null);
    try {
      const data = await getEmployee(employeeId);
      setConfig(data);
      
      // 初始化表单数据
      setFormData({
        displayName: data.displayName || '',
        description: data.description || '',
        defaultRule: data.defaultRule || '',
        roleArn: extractRoleName(data.roleArn || '')
      });

      // 初始化 SOP 配置
      if (data.knowledges && data.knowledges.sop && data.knowledges.sop.length > 0) {
        setSopConfigs(data.knowledges.sop.map(sop => ({
          type: sop.type || 'oss',
          region: sop.region || 'cn-hangzhou',
          bucket: sop.bucket || '',
          basePath: sop.basePath || '',
          organizationId: sop.organizationId || '',
          repositoryId: sop.repositoryId || '',
          branchName: sop.branchName || '',
          token: sop.token || '',
          id: sop.id || '',
          description: sop.description || ''
        })));
      } else {
        setSopConfigs([{
          type: 'oss',
          region: 'cn-hangzhou',
          bucket: '',
          basePath: '',
          organizationId: '',
          repositoryId: '',
          branchName: '',
          token: '',
          id: '',
          description: ''
        }]);
      }
    } catch (err) {
      setError(err.message);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadConfig();
  }, [employeeId]);

  // 获取 AccountID
  useEffect(() => {
    const fetchAccountId = async () => {
      try {
        const id = await getAccountId();
        setAccountId(id);
      } catch (err) {
        console.error('获取账号ID失败:', err);
      }
    };
    fetchAccountId();
  }, []);

  // 从完整 ARN 中提取角色名
  const extractRoleName = (arn) => {
    if (!arn) return '';
    if (arn.startsWith('acs:ram::')) {
      const match = arn.match(/acs:ram::\d+:role\/(.+)$/);
      if (match && match[1]) {
        return match[1];
      }
    }
    return arn;
  };

  // 构建完整的 RoleARN
  const getFullRoleArn = () => {
    if (!formData.roleArn) return '';
    if (accountId) {
      return `acs:ram::${accountId}:role/${formData.roleArn}`;
    }
    return formData.roleArn;
  };

  const handleInputChange = (e) => {
    const { name, value } = e.target;
    
    // 特殊处理 roleArn 字段
    if (name === 'roleArn') {
      if (value.startsWith('acs:ram::')) {
        const match = value.match(/acs:ram::\d+:role\/(.+)$/);
        if (match && match[1]) {
          setFormData(prev => ({ ...prev, [name]: match[1] }));
          return;
        }
      }
    }
    
    setFormData(prev => ({ ...prev, [name]: value }));
  };

  const handleSopConfigChange = (index, e) => {
    const { name, value } = e.target;
    setSopConfigs(prev => {
      const newConfigs = [...prev];
      newConfigs[index] = {
        ...newConfigs[index],
        [name]: value
      };
      return newConfigs;
    });
  };

  const addSopConfig = () => {
    setSopConfigs(prev => [...prev, {
      type: 'oss',
      region: 'cn-hangzhou',
      bucket: '',
      basePath: '',
      organizationId: '',
      repositoryId: '',
      branchName: '',
      token: '',
      id: '',
      description: ''
    }]);
  };

  const removeSopConfig = (index) => {
    if (sopConfigs.length > 1) {
      setSopConfigs(prev => prev.filter((_, i) => i !== index));
    }
  };

  const buildSopKnowledge = (sopConfig) => {
    const sop = {
      type: sopConfig.type
    };

    switch (sopConfig.type) {
      case 'oss':
        if (!sopConfig.region || !sopConfig.bucket || !sopConfig.basePath) {
          throw new Error(t('employeeSettings.ossValidation'));
        }
        sop.region = sopConfig.region;
        sop.bucket = sopConfig.bucket;
        sop.basePath = sopConfig.basePath;
        if (sopConfig.description) sop.description = sopConfig.description;
        break;

      case 'yunxiao':
        if (!sopConfig.organizationId || !sopConfig.repositoryId || !sopConfig.branchName || !sopConfig.token || !sopConfig.basePath) {
          throw new Error(t('employeeSettings.yunxiaoValidation'));
        }
        sop.organizationId = sopConfig.organizationId;
        sop.repositoryId = sopConfig.repositoryId;
        sop.branchName = sopConfig.branchName;
        sop.basePath = sopConfig.basePath;
        sop.token = sopConfig.token;
        if (sopConfig.description) sop.description = sopConfig.description;
        break;

      case 'builtin':
        if (!sopConfig.id) {
          throw new Error(t('employeeSettings.builtinValidation'));
        }
        sop.id = sopConfig.id;
        break;

      default:
        throw new Error(t('employeeSettings.unknownType'));
    }

    return sop;
  };

  const handleSave = async () => {
    setError(null);

    // 验证必填字段
    if (!formData.displayName.trim()) {
      setError(t('employeeSettings.displayNameRequired'));
      return;
    }
    if (!formData.defaultRule.trim()) {
      setError(t('employeeSettings.defaultRuleRequired'));
      return;
    }

    setSaving(true);

    try {
      // 构建所有 SOP Knowledges
      const sopKnowledges = sopConfigs.map((config, index) => {
        try {
          return buildSopKnowledge(config);
        } catch (err) {
          throw new Error(`${t('employeeSettings.knowledgeError')} ${index + 1}: ${err.message}`);
        }
      });

      // 构建更新数据
      // 注意：虽然 roleArn 不可修改，但 API 要求必须传递此参数
      const updateData = {
        displayName: formData.displayName,
        description: formData.description,
        defaultRule: formData.defaultRule,
        roleArn: config.roleArn, // 传递原始的 roleArn
        sopKnowledges: sopKnowledges
      };

      await updateEmployee(employeeId, updateData);

      // 更新成功，重新加载配置
      await loadConfig();
      setEditMode(false);
    } catch (err) {
      setError(err.message);
    } finally {
      setSaving(false);
    }
  };

  const handleCancel = () => {
    // 恢复原始数据
    setFormData({
      displayName: config.displayName || '',
      description: config.description || '',
      defaultRule: config.defaultRule || '',
      roleArn: extractRoleName(config.roleArn || '')
    });

    // 恢复 SOP 配置
    if (config.knowledges && config.knowledges.sop && config.knowledges.sop.length > 0) {
      setSopConfigs(config.knowledges.sop.map(sop => ({
        type: sop.type || 'oss',
        region: sop.region || 'cn-hangzhou',
        bucket: sop.bucket || '',
        basePath: sop.basePath || '',
        organizationId: sop.organizationId || '',
        repositoryId: sop.repositoryId || '',
        branchName: sop.branchName || '',
        token: sop.token || '',
        id: sop.id || '',
        description: sop.description || ''
      })));
    }

    setEditMode(false);
    setError(null);
  };

  const renderKnowledgeItem = (knowledge, index) => {
    const type = knowledge.type || 'unknown';
    const typeColors = {
      oss: { bg: '#f0f5ff', accent: '#2f54eb', icon: '☁️', label: t('employeeSettings.ossLabel') },
      yunxiao: { bg: '#f6ffed', accent: '#52c41a', icon: '🌿', label: t('employeeSettings.yunxiaoLabel') },
      builtin: { bg: '#fff7e6', accent: '#fa8c16', icon: '📚', label: t('employeeSettings.builtinLabel') }
    };
    const style = typeColors[type] || typeColors.builtin;

    return (
      <div 
        key={index}
        style={{
          marginBottom: '20px',
          backgroundColor: 'white',
          borderRadius: '12px',
          border: '1px solid #e8e8e8',
          overflow: 'hidden',
          boxShadow: '0 2px 8px rgba(0,0,0,0.04)'
        }}
      >
        {/* Header */}
        <div style={{
          padding: '14px 20px',
          backgroundColor: style.bg,
          borderBottom: '1px solid #e8e8e8',
          display: 'flex',
          alignItems: 'center',
          gap: '10px'
        }}>
          <span style={{ fontSize: '18px' }}>{style.icon}</span>
          <strong style={{ color: '#333', fontSize: '15px' }}>{t('employeeSettings.knowledge')} {index + 1}</strong>
          <span style={{
            marginLeft: 'auto',
            padding: '2px 10px',
            borderRadius: '12px',
            backgroundColor: style.accent,
            color: 'white',
            fontSize: '12px',
            fontWeight: 600
          }}>
            {style.label}
          </span>
        </div>
        
        {/* Content */}
        <div style={{ padding: '16px 20px' }}>
          {type === 'oss' && (
            <>
              <div className="info-row">
                <label>{t('employeeSettings.regionLabel')}</label>
                <span className="info-value code">{knowledge.region || '-'}</span>
              </div>
              <div className="info-row">
                <label>{t('employeeSettings.bucketLabel')}</label>
                <span className="info-value code">{knowledge.bucket || '-'}</span>
              </div>
              <div className="info-row">
                <label>{t('employeeSettings.basePathLabel')}</label>
                <span className="info-value code">{knowledge.basePath || '-'}</span>
              </div>
              {knowledge.description && (
                <div className="info-row">
                  <label>{t('employeeSettings.knowledgeDesc')}</label>
                  <span className="info-value">{knowledge.description}</span>
                </div>
              )}
            </>
          )}
          
          {type === 'yunxiao' && (
            <>
              <div className="info-row">
                <label>{t('employeeSettings.orgId')}</label>
                <span className="info-value code">{knowledge.organizationId || '-'}</span>
              </div>
              <div className="info-row">
                <label>{t('employeeSettings.repoId')}</label>
                <span className="info-value code">{knowledge.repositoryId || '-'}</span>
              </div>
              <div className="info-row">
                <label>{t('employeeSettings.branchName')}</label>
                <span className="info-value code">{knowledge.branchName || '-'}</span>
              </div>
              <div className="info-row">
                <label>{t('employeeSettings.basePathLabel')}</label>
                <span className="info-value code">{knowledge.basePath || '-'}</span>
              </div>
              {knowledge.description && (
                <div className="info-row">
                  <label>{t('employeeSettings.knowledgeDesc')}</label>
                  <span className="info-value">{knowledge.description}</span>
                </div>
              )}
            </>
          )}
          
          {type === 'builtin' && (
            <div className="info-row">
              <label>{t('employeeSettings.knowledgeId')}</label>
              <span className="info-value code">{knowledge.id || '-'}</span>
            </div>
          )}
        </div>
      </div>
    );
  };

  const renderEditKnowledgeItem = (knowledge, index) => {
    return (
      <div key={index} className="mcp-server-form" style={{ marginBottom: '20px' }}>
        <div className="mcp-server-header">
          <span>{t('employeeSettings.knowledge')} {index + 1}</span>
          {sopConfigs.length > 1 && (
            <button
              type="button"
              className="remove-btn"
              onClick={() => removeSopConfig(index)}
            >
              {t('employeeSettings.deleteKnowledge')}
            </button>
          )}
        </div>

        <div className="form-group">
          <label>{t('employeeSettings.knowledgeType')} <span className="required">*</span></label>
          <select
            name="type"
            value={knowledge.type}
            onChange={(e) => handleSopConfigChange(index, e)}
          >
            <option value="oss">{t('employeeSettings.ossLabel')}</option>
            <option value="yunxiao">{t('employeeSettings.yunxiaoLabel')}</option>
            <option value="builtin">{t('employeeSettings.builtinLabel')}</option>
          </select>
        </div>

        {knowledge.type === 'oss' && (
          <>
            <div className="form-group">
              <label>{t('employeeSettings.regionLabel')} <span className="required">*</span></label>
              <input
                type="text"
                name="region"
                value={knowledge.region}
                onChange={(e) => handleSopConfigChange(index, e)}
                placeholder={t('employeeSettings.regionPlaceholder')}
              />
              <span className="form-hint">{t('employeeSettings.regionHint')}</span>
            </div>

            <div className="form-group">
              <label>{t('employeeSettings.bucketLabel')} <span className="required">*</span></label>
              <input
                type="text"
                name="bucket"
                value={knowledge.bucket}
                onChange={(e) => handleSopConfigChange(index, e)}
                placeholder={t('employeeSettings.bucketPlaceholder')}
              />
            </div>

            <div className="form-group">
              <label>{t('employeeSettings.basePathLabel')} <span className="required">*</span></label>
              <input
                type="text"
                name="basePath"
                value={knowledge.basePath}
                onChange={(e) => handleSopConfigChange(index, e)}
                placeholder={t('employeeSettings.basePathPlaceholder')}
              />
              <span className="form-hint">{t('employeeSettings.basePathHintOss')}</span>
            </div>

            <div className="form-group">
              <label>{t('employeeSettings.knowledgeDesc')}</label>
              <textarea
                name="description"
                value={knowledge.description}
                onChange={(e) => handleSopConfigChange(index, e)}
                placeholder={t('employeeSettings.knowledgeDescPlaceholder')}
                rows={2}
              />
            </div>
          </>
        )}

        {knowledge.type === 'yunxiao' && (
          <>
            <div className="form-group">
              <label>{t('employeeSettings.orgId')} <span className="required">*</span></label>
              <input
                type="text"
                name="organizationId"
                value={knowledge.organizationId}
                onChange={(e) => handleSopConfigChange(index, e)}
                placeholder={t('employeeSettings.orgId')}
              />
            </div>

            <div className="form-group">
              <label>{t('employeeSettings.repoId')} <span className="required">*</span></label>
              <input
                type="text"
                name="repositoryId"
                value={knowledge.repositoryId}
                onChange={(e) => handleSopConfigChange(index, e)}
                placeholder={t('employeeSettings.repoId')}
              />
            </div>

            <div className="form-group">
              <label>{t('employeeSettings.branchName')} <span className="required">*</span></label>
              <input
                type="text"
                name="branchName"
                value={knowledge.branchName}
                onChange={(e) => handleSopConfigChange(index, e)}
                placeholder={t('employeeSettings.branchPlaceholder')}
              />
            </div>

            <div className="form-group">
              <label>{t('employeeSettings.basePathLabel')} <span className="required">*</span></label>
              <input
                type="text"
                name="basePath"
                value={knowledge.basePath}
                onChange={(e) => handleSopConfigChange(index, e)}
                placeholder={t('employeeSettings.basePathPlaceholder')}
              />
              <span className="form-hint">{t('employeeSettings.basePathHintYunxiao')}</span>
            </div>

            <div className="form-group">
              <label>{t('employeeSettings.accessToken')} <span className="required">*</span></label>
              <input
                type="password"
                name="token"
                value={knowledge.token}
                onChange={(e) => handleSopConfigChange(index, e)}
                placeholder={t('employeeSettings.accessToken')}
              />
            </div>

            <div className="form-group">
              <label>{t('employeeSettings.knowledgeDesc')}</label>
              <textarea
                name="description"
                value={knowledge.description}
                onChange={(e) => handleSopConfigChange(index, e)}
                placeholder={t('employeeSettings.knowledgeDescPlaceholder')}
                rows={2}
              />
            </div>
          </>
        )}

        {knowledge.type === 'builtin' && (
          <div className="form-group">
            <label>{t('employeeSettings.knowledgeId')} <span className="required">*</span></label>
            <input
              type="text"
              name="id"
              value={knowledge.id}
              onChange={(e) => handleSopConfigChange(index, e)}
              placeholder={t('employeeSettings.knowledgeId')}
            />
          </div>
        )}
      </div>
    );
  };

  const renderInfoTab = () => {
    if (editMode) {
      return (
        <div className="settings-page-info">
          <div className="create-form-section">
            <div className="form-group">
              <label>{t('employeeSettings.employeeName')}</label>
              <input
                type="text"
                value={config.name}
                disabled
                style={{ backgroundColor: '#f5f5f5', cursor: 'not-allowed' }}
              />
              <span className="form-hint">{t('employeeSettings.employeeNameReadonly')}</span>
            </div>

            <div className="form-group">
              <label>{t('employeeSettings.displayName')} <span className="required">*</span></label>
              <input
                type="text"
                name="displayName"
                value={formData.displayName}
                onChange={handleInputChange}
                placeholder={t('createEmployee.displayNamePlaceholder')}
              />
            </div>

            <div className="form-group">
              <label>{t('employeeSettings.description')}</label>
              <input
                type="text"
                name="description"
                value={formData.description}
                onChange={handleInputChange}
                placeholder={t('employeeSettings.descPlaceholder')}
              />
            </div>

            <div className="form-group">
              <label>{t('employeeSettings.roleArn')}</label>
              <input
                type="text"
                value={config.roleArn || ''}
                disabled
                style={{ backgroundColor: '#f5f5f5', cursor: 'not-allowed', fontFamily: 'monospace' }}
              />
              <span className="form-hint">{t('employeeSettings.roleArnReadonly')}</span>
            </div>

            <div className="form-group">
              <label>{t('employeeSettings.defaultRule')} <span className="required">*</span></label>
              <textarea
                name="defaultRule"
                value={formData.defaultRule}
                onChange={handleInputChange}
                placeholder={t('employeeSettings.defaultRulePlaceholder')}
                rows={6}
              />
            </div>
          </div>
        </div>
      );
    }

    return (
      <div className="settings-page-info">
        <div className="info-cards">
          {/* Basic Info */}
          <div className="info-card wide">
            <div className="info-card-header">
              <span className="info-card-icon">📋</span>
              <h4>{t('employeeSettings.basicInfo')}</h4>
            </div>
            <div className="info-card-content">
              <div className="info-row">
                <label>{t('employeeSettings.employeeName')}</label>
                <span className="info-value code">{config.name}</span>
              </div>
              <div className="info-row">
                <label>{t('employeeSettings.displayName')}</label>
                <span className="info-value">{config.displayName}</span>
              </div>
              {config.description && (
                <div className="info-row">
                  <label>{t('employeeSettings.description')}</label>
                  <span className="info-value">{config.description}</span>
                </div>
              )}
              {config.roleArn && (
                <div className="info-row">
                  <label>{t('employeeSettings.roleArn')}</label>
                  <span className="info-value code">{config.roleArn}</span>
                </div>
              )}
              {config.employeeType && (
                <div className="info-row">
                  <label>{t('employeeSettings.employeeType')}</label>
                  <span className="info-value">{config.employeeType}</span>
                </div>
              )}
              {config.regionId && (
                <div className="info-row">
                  <label>{t('employeeSettings.regionId')}</label>
                  <span className="info-value code">{config.regionId}</span>
                </div>
              )}
              {config.createTime && (
                <div className="info-row">
                  <label>{t('employeeSettings.createTime')}</label>
                  <span className="info-value">{formatTime(config.createTime)}</span>
                </div>
              )}
              {config.updateTime && (
                <div className="info-row">
                  <label>{t('employeeSettings.updateTime')}</label>
                  <span className="info-value">{formatTime(config.updateTime)}</span>
                </div>
              )}
            </div>
          </div>

          {/* Role Definition */}
          {config.defaultRule && (
            <div className="info-card wide">
              <div className="info-card-header">
                <span className="info-card-icon">🤖</span>
                <h4>{t('employeeSettings.roleDefinition')}</h4>
              </div>
              <div className="info-card-content">
                <div className="role-desc-content">{config.defaultRule}</div>
              </div>
            </div>
          )}
        </div>
      </div>
    );
  };

  const renderKnowledgesTab = () => {
    if (editMode) {
      return (
        <div className="settings-page-info">
          <div className="create-form-section">
            {sopConfigs.map((sopConfig, index) => 
              renderEditKnowledgeItem(sopConfig, index)
            )}
            
            <button type="button" className="add-btn" onClick={addSopConfig}>
              + {t('employeeSettings.addKnowledge')}
            </button>
          </div>
        </div>
      );
    }

    if (!config.knowledges || !config.knowledges.sop || config.knowledges.sop.length === 0) {
      return (
        <div className="settings-page-info">
          <div style={{
            padding: '60px 20px',
            textAlign: 'center',
            color: '#999',
            backgroundColor: 'white',
            borderRadius: '12px',
            border: '1px solid #e8e8e8'
          }}>
            <div style={{ fontSize: '48px', marginBottom: '16px' }}>📚</div>
            <p>{t('employeeSettings.noKnowledges')}</p>
          </div>
        </div>
      );
    }

    return (
      <div className="settings-page-info">
        {/* Summary */}
        <div style={{
          padding: '20px 24px',
          marginBottom: '24px',
          backgroundColor: 'white',
          borderRadius: '12px',
          border: '1px solid #e8e8e8',
          boxShadow: '0 2px 8px rgba(0,0,0,0.04)'
        }}>
          <div style={{ textAlign: 'center' }}>
            <div style={{fontSize: '32px', fontWeight: 700, color: '#667eea'}}>
              {config.knowledges.sop.length}
            </div>
            <div style={{fontSize: '13px', color: '#888', marginTop: '4px'}}>
              {t('employeeSettings.configuredKnowledges')}
            </div>
          </div>
        </div>

        {/* SOP Knowledges */}
        {config.knowledges.sop.map((knowledge, index) => 
          renderKnowledgeItem(knowledge, index)
        )}
      </div>
    );
  };

  if (loading) {
    return (
      <div className="settings-page">
        <div className="settings-page-header">
          <div className="header-left">
            <button onClick={() => navigate('/')} className="back-button">
              ← {t('employeeSettings.backButton')}
            </button>
            <h2>⚙️ {t('employeeSettings.settingsTitle')}</h2>
          </div>
        </div>
        <div className="settings-page-content">
          <div className="settings-page-loading">
            <div className="loading-spinner"></div>
            <p>{t('employeeSettings.loadingConfig')}</p>
          </div>
        </div>
      </div>
    );
  }

  if (error && !config) {
    return (
      <div className="settings-page">
        <div className="settings-page-header">
          <div className="header-left">
            <button onClick={() => navigate('/')} className="back-button">
              ← {t('employeeSettings.backButton')}
            </button>
            <h2>⚙️ {t('employeeSettings.settingsTitle')}</h2>
          </div>
        </div>
        <div className="settings-page-content">
          <div className="settings-page-error">{error}</div>
        </div>
      </div>
    );
  }

  if (!config) return null;

  return (
    <div className="settings-page">
      <div className="settings-page-header">
        <div className="header-left">
          <button onClick={() => navigate('/')} className="back-button">
            ← {t('employeeSettings.backButton')}
          </button>
          <h2>⚙️ {config.displayName || config.name} - {t('employeeSettings.settings')}</h2>
        </div>
        <div className="header-actions">
          {isAdmin && (
            !editMode ? (
              <button className="btn-primary" onClick={() => setEditMode(true)}>
                ✏️ {t('employeeSettings.edit')}
              </button>
            ) : (
              <>
                <button 
                  className="btn-secondary" 
                  onClick={handleCancel}
                  disabled={saving}
                >
                  {t('employeeSettings.cancel')}
                </button>
                <button 
                  className="btn-primary" 
                  onClick={handleSave}
                  disabled={saving}
                >
                  {saving ? t('employeeSettings.saving') : `💾 ${t('employeeSettings.save')}`}
                </button>
              </>
            )
          )}
        </div>
      </div>

      <div className="settings-page-tabs">
        <button 
          className={`page-tab ${activeTab === 'info' ? 'active' : ''}`}
          onClick={() => setActiveTab('info')}
        >
          📋 {t('employeeSettings.basicInfo')}
        </button>
        <button 
          className={`page-tab ${activeTab === 'knowledges' ? 'active' : ''}`}
          onClick={() => setActiveTab('knowledges')}
        >
          📚 {t('employeeSettings.knowledgesTab')}
        </button>
      </div>
      
      <div className="settings-page-content">
        {error && editMode && (
          <div className="form-error" style={{ marginBottom: '20px' }}>{error}</div>
        )}
        {activeTab === 'info' && renderInfoTab()}
        {activeTab === 'knowledges' && renderKnowledgesTab()}
      </div>
    </div>
  );
};

// Helper function to format time
const formatTime = (timeString) => {
  if (!timeString) return '-';
  try {
    const date = new Date(timeString);
    return date.toLocaleString(undefined, {
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
      hour12: false
    });
  } catch (e) {
    return timeString;
  }
};

export default EmployeeSettings;
