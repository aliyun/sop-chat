/**
 * CreateEmployee Component
 * Form to create a new digital employee
 */
import React, { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { createEmployee, getAccountId } from '../services/api';
import { useAuth } from '../contexts/AuthContext';
import { useTranslation } from 'react-i18next';

const CreateEmployee = () => {
  const navigate = useNavigate();
  const { user } = useAuth();
  
  // 检查用户是否有 admin 角色
  // 注意：后端返回的字段是大写开头的 (Roles)，也要兼容小写 (roles)
  const userRoles = user?.Roles || user?.roles || [];
  const isAdmin = userRoles.includes('admin');
  const { t } = useTranslation();
  
  // 如果不是 admin，跳转回首页
  useEffect(() => {
    if (!isAdmin) {
      navigate('/', { replace: true });
    }
  }, [isAdmin, navigate]);
  const [formData, setFormData] = useState({
    name: '',
    displayName: '',
    defaultRule: '',
    roleArn: 'aliyunserviceroleforcloudmonitor'
  });

  // SOP 知识库配置（支持多个）
  const [sopConfigs, setSopConfigs] = useState([
    {
      type: 'builtin', // oss, yunxiao, builtin
      // OSS 类型字段
      region: 'cn-hangzhou',
      bucket: '',
      basePath: '',
      // Yunxiao 类型字段
      organizationId: '',
      repositoryId: '',
      branchName: '',
      token: '',
      // Builtin 类型字段
      id: 'sls',
      // 通用字段
      description: ''
    }
  ]);

  const [creating, setCreating] = useState(false);
  const [error, setError] = useState(null);
  const [step, setStep] = useState(1); // 1: 基本信息, 2: SOP配置
  const [accountId, setAccountId] = useState(null);
  const [loadingAccountId, setLoadingAccountId] = useState(false);

  // 在组件加载时获取 AccountID
  useEffect(() => {
    const fetchAccountId = async () => {
      setLoadingAccountId(true);
      try {
        const id = await getAccountId();
        setAccountId(id);
      } catch (err) {
        console.error('获取账号ID失败:', err);
        // 失败也不影响用户输入，用户可以输入完整的 ARN
      } finally {
        setLoadingAccountId(false);
      }
    };

    fetchAccountId();
  }, []);

  // 计算完整的 RoleARN（用于提交）
  const getFullRoleArn = () => {
    if (!formData.roleArn) return '';

    // 如果有 accountId，构建完整的 ARN
    if (accountId) {
      return `acs:ram::${accountId}:role/${formData.roleArn}`;
    }

    // 如果还没有获取到 accountId，返回角色名（后端会处理）
    return formData.roleArn;
  };

  const handleInputChange = (e) => {
    const { name, value } = e.target;

    // 特殊处理 roleArn 字段
    if (name === 'roleArn') {
      // 如果用户粘贴了完整的 ARN (acs:ram::xxxxx:role/yyy)，提取角色名部分
      if (value.startsWith('acs:ram::')) {
        const match = value.match(/acs:ram::\d+:role\/(.+)$/);
        if (match && match[1]) {
          // 只保存角色名部分
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
          throw new Error(t('createEmployee.ossValidation'));
        }
        sop.region = sopConfig.region;
        sop.bucket = sopConfig.bucket;
        sop.basePath = sopConfig.basePath;
        if (sopConfig.description) sop.description = sopConfig.description;
        break;

      case 'yunxiao':
        if (!sopConfig.organizationId || !sopConfig.repositoryId || !sopConfig.branchName || !sopConfig.token || !sopConfig.basePath) {
          throw new Error(t('createEmployee.yunxiaoValidation'));
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
          throw new Error(t('createEmployee.builtinValidation'));
        }
        sop.id = sopConfig.id;
        break;

      default:
        throw new Error(t('createEmployee.unknownType'));
    }

    return sop;
  };

  const handleSubmit = async () => {
    setError(null);

    // 验证必填字段
    if (!formData.name.trim()) {
      setError(t('createEmployee.nameRequired'));
      return;
    }
    if (!formData.displayName.trim()) {
      setError(t('createEmployee.displayNameRequired'));
      return;
    }
    if (!formData.roleArn.trim()) {
      setError(t('createEmployee.roleArnRequired'));
      return;
    }
    if (!formData.defaultRule.trim()) {
      setError(t('createEmployee.defaultRuleRequired'));
      return;
    }

    // 验证员工名称格式
    if (!/^[a-zA-Z0-9_-]+$/.test(formData.name)) {
      setError(t('createEmployee.nameFormat'));
      return;
    }

    setCreating(true);

    try {
      // 构建所有 SOP Knowledges（必填）
      const sopKnowledges = sopConfigs.map((config, index) => {
        try {
          return buildSopKnowledge(config);
        } catch (err) {
          throw new Error(`${t('createEmployee.knowledgeError')} ${index + 1}: ${err.message}`);
        }
      });

      // 构建员工数据，使用完整的 RoleARN
      const employeeData = {
        ...formData,
        roleArn: getFullRoleArn(),
        sopKnowledges: sopKnowledges
      };

      const result = await createEmployee(employeeData);

      // 创建成功，跳转回员工选择页面
      navigate('/');

    } catch (err) {
      setError(err.message);
    } finally {
      setCreating(false);
    }
  };

  const renderStep1 = () => (
    <div className="create-form-section">
      <div className="form-group">
        <label>{t('createEmployee.employeeName')} <span className="required">*</span></label>
        <input
          type="text"
          name="name"
          value={formData.name}
          onChange={handleInputChange}
          placeholder={t('createEmployee.employeeNamePlaceholder')}
        />
        <span className="form-hint">{t('createEmployee.employeeNameHint')}</span>
      </div>

      <div className="form-group">
        <label>{t('createEmployee.displayName')} <span className="required">*</span></label>
        <input
          type="text"
          name="displayName"
          value={formData.displayName}
          onChange={handleInputChange}
          placeholder={t('createEmployee.displayNamePlaceholder')}
        />
        <span className="form-hint">{t('createEmployee.displayNameHint')}</span>
      </div>

      <div className="form-group">
        <label>{t('createEmployee.roleArn')} <span className="required">*</span></label>
        <div style={{ display: 'flex', alignItems: 'stretch' }}>
          {/* 前缀部分 */}
          <div style={{
            display: 'flex',
            alignItems: 'center',
            padding: '8px 12px',
            backgroundColor: '#f5f5f5',
            border: '1px solid #ddd',
            borderRight: 'none',
            borderRadius: '4px 0 0 4px',
            fontSize: '14px',
            fontFamily: 'monospace',
            color: '#666',
            whiteSpace: 'nowrap'
          }}>
            {loadingAccountId ? (
              <span style={{ fontSize: '12px' }}>{t('createEmployee.loadingAccountId')}</span>
            ) : accountId ? (
              <span>acs:ram::{accountId}:role/</span>
            ) : (
              <span>acs:ram::&lt;{t('createEmployee.accountIdPrefix')}&gt;:role/</span>
            )}
          </div>
          {/* 输入框部分 */}
          <input
            type="text"
            name="roleArn"
            value={formData.roleArn}
            onChange={handleInputChange}
            placeholder={t('createEmployee.roleArnInputPlaceholder')}
            style={{
              flex: 1,
              borderRadius: '0 4px 4px 0',
              fontFamily: 'monospace'
            }}
          />
        </div>
        {/* 显示完整的 RoleARN */}
        {formData.roleArn && (
          <div style={{
            marginTop: '8px',
            padding: '6px 10px',
            backgroundColor: '#f0f7ff',
            border: '1px solid #d0e4ff',
            borderRadius: '4px',
            fontSize: '12px',
            color: '#0066cc',
            lineHeight: '1.4'
          }}>
            <strong style={{ fontSize: '12px' }}>{t('createEmployee.fullRoleArn')}</strong>
            <span style={{ marginLeft: '6px', fontFamily: 'monospace', fontSize: '11px', wordBreak: 'break-all' }}>
              {getFullRoleArn()}
            </span>
          </div>
        )}
        <span className="form-hint">
          {t('createEmployee.roleArnHint')}
        </span>
      </div>

      <div className="form-group">
        <label>{t('createEmployee.defaultRule')} <span className="required">*</span></label>
        <textarea
          name="defaultRule"
          value={formData.defaultRule}
          onChange={handleInputChange}
          placeholder={t('createEmployee.defaultRulePlaceholder')}
          rows={4}
        />
        <span className="form-hint">{t('createEmployee.defaultRuleHint')}</span>
      </div>
    </div>
  );

  const renderStep2 = () => (
    <div className="create-form-section">
      {sopConfigs.map((sopConfig, index) => (
        <div key={index} className="mcp-server-form">
          <div className="mcp-server-header">
            <span>{t('createEmployee.knowledge')} {index + 1}</span>
            {sopConfigs.length > 1 && (
              <button
                type="button"
                className="remove-btn"
                onClick={() => removeSopConfig(index)}
              >
                {t('createEmployee.deleteKnowledge')}
              </button>
            )}
          </div>

          <div className="form-group">
            <label>{t('createEmployee.knowledgeType')} <span className="required">*</span></label>
            <select
              name="type"
              value={sopConfig.type}
              onChange={(e) => handleSopConfigChange(index, e)}
            >
              <option value="oss">{t('createEmployee.ossType')}</option>
              <option value="yunxiao">{t('createEmployee.yunxiaoType')}</option>
              <option value="builtin">{t('createEmployee.builtinType')}</option>
            </select>
          </div>

          {sopConfig.type === 'oss' && (
            <>
              <div className="form-group">
                <label>{t('createEmployee.regionLabel')} <span className="required">*</span></label>
                <input
                  type="text"
                  name="region"
                  value={sopConfig.region}
                  onChange={(e) => handleSopConfigChange(index, e)}
                  placeholder={t('createEmployee.regionPlaceholder')}
                  required
                />
                <span className="form-hint">{t('createEmployee.regionHint')}</span>
              </div>

              <div className="form-group">
                <label>{t('createEmployee.bucketLabel')} <span className="required">*</span></label>
                <input
                  type="text"
                  name="bucket"
                  value={sopConfig.bucket}
                  onChange={(e) => handleSopConfigChange(index, e)}
                  placeholder={t('createEmployee.bucketPlaceholder')}
                />
              </div>

              <div className="form-group">
                <label>{t('createEmployee.basePathLabel')} <span className="required">*</span></label>
                <input
                  type="text"
                  name="basePath"
                  value={sopConfig.basePath}
                  onChange={(e) => handleSopConfigChange(index, e)}
                  placeholder={t('createEmployee.basePathPlaceholder')}
                />
                <span className="form-hint">{t('createEmployee.basePathHintOss')}</span>
              </div>

              <div className="form-group">
                <label>{t('createEmployee.knowledgeDesc')}</label>
                <textarea
                  name="description"
                  value={sopConfig.description}
                  onChange={(e) => handleSopConfigChange(index, e)}
                  placeholder={t('createEmployee.knowledgeDescPlaceholder')}
                  rows={2}
                />
              </div>
            </>
          )}

          {sopConfig.type === 'yunxiao' && (
            <>
              <div className="form-group">
                <label>{t('createEmployee.orgId')} <span className="required">*</span></label>
                <input
                  type="text"
                  name="organizationId"
                  value={sopConfig.organizationId}
                  onChange={(e) => handleSopConfigChange(index, e)}
                  placeholder={t('createEmployee.orgId')}
                />
              </div>

              <div className="form-group">
                <label>{t('createEmployee.repoId')} <span className="required">*</span></label>
                <input
                  type="text"
                  name="repositoryId"
                  value={sopConfig.repositoryId}
                  onChange={(e) => handleSopConfigChange(index, e)}
                  placeholder={t('createEmployee.repoId')}
                />
              </div>

              <div className="form-group">
                <label>{t('createEmployee.branchName')} <span className="required">*</span></label>
                <input
                  type="text"
                  name="branchName"
                  value={sopConfig.branchName}
                  onChange={(e) => handleSopConfigChange(index, e)}
                  placeholder={t('createEmployee.branchPlaceholder')}
                />
              </div>

              <div className="form-group">
                <label>{t('createEmployee.basePathLabel')} <span className="required">*</span></label>
                <input
                  type="text"
                  name="basePath"
                  value={sopConfig.basePath}
                  onChange={(e) => handleSopConfigChange(index, e)}
                  placeholder={t('createEmployee.basePathPlaceholder')}
                />
                <span className="form-hint">{t('createEmployee.basePathHintYunxiao')}</span>
              </div>

              <div className="form-group">
                <label>{t('createEmployee.accessToken')} <span className="required">*</span></label>
                <input
                  type="password"
                  name="token"
                  value={sopConfig.token}
                  onChange={(e) => handleSopConfigChange(index, e)}
                  placeholder={t('createEmployee.accessToken')}
                />
              </div>

              <div className="form-group">
                <label>{t('createEmployee.knowledgeDesc')}</label>
                <textarea
                  name="description"
                  value={sopConfig.description}
                  onChange={(e) => handleSopConfigChange(index, e)}
                  placeholder={t('createEmployee.knowledgeDescPlaceholder')}
                  rows={2}
                />
              </div>
            </>
          )}

          {sopConfig.type === 'builtin' && (
            <div className="form-group">
              <label>{t('createEmployee.knowledgeId')} <span className="required">*</span></label>
              <input
                type="text"
                name="id"
                value={sopConfig.id}
                onChange={(e) => handleSopConfigChange(index, e)}
                placeholder={t('createEmployee.knowledgeId')}
              />
            </div>
          )}
        </div>
      ))}

      <button type="button" className="add-btn" onClick={addSopConfig}>
        + {t('createEmployee.addKnowledge')}
      </button>
    </div>
  );

  // 如果不是 admin，不渲染任何内容（等待跳转）
  if (!isAdmin) {
    return null;
  }

  return (
    <div className="create-app-page">
      <div className="create-app-header">
        <div className="header-left">
          <button onClick={() => navigate('/')} className="back-button">
            ← {t('createEmployee.backButton')}
          </button>
          <h2>➕ {t('createEmployee.pageTitle')}</h2>
        </div>
      </div>

      <div className="create-app-content">
        <div className="create-steps">
          <div className={`step ${step >= 1 ? 'active' : ''} ${step > 1 ? 'completed' : ''}`}>
            <span className="step-number">1</span>
            <span className="step-label">{t('createEmployee.step1')}</span>
          </div>
          <div className="step-line"></div>
          <div className={`step ${step >= 2 ? 'active' : ''}`}>
            <span className="step-number">2</span>
            <span className="step-label">{t('createEmployee.step2')}</span>
          </div>
        </div>

        <div className="create-form">
          {step === 1 && renderStep1()}
          {step === 2 && renderStep2()}

          {error && <div className="form-error">{error}</div>}

          <div className="form-actions">
            {step > 1 && (
              <button
                type="button"
                className="btn-secondary"
                onClick={() => setStep(step - 1)}
                disabled={creating}
              >
                {t('createEmployee.prevStep')}
              </button>
            )}

            {step < 2 ? (
              <button
                type="button"
                className="btn-primary"
                onClick={() => setStep(step + 1)}
              >
                {t('createEmployee.nextStep')}
              </button>
            ) : (
              <button
                type="button"
                className="btn-primary"
                onClick={handleSubmit}
                disabled={creating}
              >
                {creating ? t('createEmployee.creating') : t('createEmployee.createButton')}
              </button>
            )}
          </div>
        </div>
      </div>
    </div>
  );
};

export default CreateEmployee;
