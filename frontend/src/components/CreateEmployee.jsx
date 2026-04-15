/**
 * CreateEmployee Component
 * Form to create a new digital employee
 */
import React, { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { createEmployee, getAccountId } from '../services/api';
import { useAuth } from '../contexts/AuthContext';

const CreateEmployee = () => {
  const navigate = useNavigate();
  const { user } = useAuth();
  
  // 检查用户是否有 admin 角色
  // 注意：后端返回的字段是大写开头的 (Roles)，也要兼容小写 (roles)
  const userRoles = user?.Roles || user?.roles || [];
  const isAdmin = userRoles.includes('admin');
  
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
          throw new Error('OSS 类型需要填写 Region、Bucket 和 Base Path');
        }
        sop.region = sopConfig.region;
        sop.bucket = sopConfig.bucket;
        sop.basePath = sopConfig.basePath;
        if (sopConfig.description) sop.description = sopConfig.description;
        break;

      case 'yunxiao':
        if (!sopConfig.organizationId || !sopConfig.repositoryId || !sopConfig.branchName || !sopConfig.token || !sopConfig.basePath) {
          throw new Error('云效类型需要填写组织ID、仓库ID、分支名称、Base Path 和 Token');
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
          throw new Error('内置类型需要填写知识库 ID');
        }
        sop.id = sopConfig.id;
        break;

      default:
        throw new Error('未知的 SOP 类型');
    }

    return sop;
  };

  const handleSubmit = async () => {
    setError(null);

    // 验证必填字段
    if (!formData.name.trim()) {
      setError('请输入员工名称');
      return;
    }
    if (!formData.displayName.trim()) {
      setError('请输入显示名称');
      return;
    }
    if (!formData.roleArn.trim()) {
      setError('请输入 Role ARN 或角色名');
      return;
    }
    if (!formData.defaultRule.trim()) {
      setError('请输入角色定义');
      return;
    }

    // 验证员工名称格式
    if (!/^[a-zA-Z0-9_-]+$/.test(formData.name)) {
      setError('员工名称只能包含字母、数字、下划线和横线');
      return;
    }

    setCreating(true);

    try {
      // 构建所有 SOP Knowledges（必填）
      const sopKnowledges = sopConfigs.map((config, index) => {
        try {
          return buildSopKnowledge(config);
        } catch (err) {
          throw new Error(`知识库 ${index + 1}: ${err.message}`);
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
        <label>员工名称 <span className="required">*</span></label>
        <input
          type="text"
          name="name"
          value={formData.name}
          onChange={handleInputChange}
          placeholder="例如：sop-test01（只能包含字母、数字、下划线、横线）"
        />
        <span className="form-hint">用于系统内部标识，建议以 sop- 开头</span>
      </div>

      <div className="form-group">
        <label>显示名称 <span className="required">*</span></label>
        <input
          type="text"
          name="displayName"
          value={formData.displayName}
          onChange={handleInputChange}
          placeholder="例如：OSS日志分析助手"
        />
        <span className="form-hint">用户界面显示的名称</span>
      </div>

      <div className="form-group">
        <label>Role ARN <span className="required">*</span></label>
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
              <span style={{ fontSize: '12px' }}>获取中...</span>
            ) : accountId ? (
              <span>acs:ram::{accountId}:role/</span>
            ) : (
              <span>acs:ram::&lt;账号ID&gt;:role/</span>
            )}
          </div>
          {/* 输入框部分 */}
          <input
            type="text"
            name="roleArn"
            value={formData.roleArn}
            onChange={handleInputChange}
            placeholder="digital-employee-role"
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
            <strong style={{ fontSize: '12px' }}>完整 RoleARN：</strong>
            <span style={{ marginLeft: '6px', fontFamily: 'monospace', fontSize: '11px', wordBreak: 'break-all' }}>
              {getFullRoleArn()}
            </span>
          </div>
        )}
        <span className="form-hint">
          只需填写角色名称（如 digital-employee-role），前缀会自动添加
        </span>
      </div>

      <div className="form-group">
        <label>角色定义 <span className="required">*</span></label>
        <textarea
          name="defaultRule"
          value={formData.defaultRule}
          onChange={handleInputChange}
          placeholder="例如：你是一个专业的 OSS 日志分析工程师，擅长排查日志相关问题..."
          rows={4}
        />
        <span className="form-hint">定义SOP问答助手的角色、专业领域和行为规则</span>
      </div>
    </div>
  );

  const renderStep2 = () => (
    <div className="create-form-section">
      {sopConfigs.map((sopConfig, index) => (
        <div key={index} className="mcp-server-form">
          <div className="mcp-server-header">
            <span>知识库 {index + 1}</span>
            {sopConfigs.length > 1 && (
              <button
                type="button"
                className="remove-btn"
                onClick={() => removeSopConfig(index)}
              >
                删除
              </button>
            )}
          </div>

          <div className="form-group">
            <label>知识库类型 <span className="required">*</span></label>
            <select
              name="type"
              value={sopConfig.type}
              onChange={(e) => handleSopConfigChange(index, e)}
            >
              <option value="oss">OSS 对象存储</option>
              <option value="yunxiao">云效代码库</option>
              <option value="builtin">内置知识库</option>
            </select>
          </div>

          {sopConfig.type === 'oss' && (
            <>
              <div className="form-group">
                <label>Region <span className="required">*</span></label>
                <input
                  type="text"
                  name="region"
                  value={sopConfig.region}
                  onChange={(e) => handleSopConfigChange(index, e)}
                  placeholder="例如: cn-hangzhou"
                  required
                />
                <span className="form-hint">OSS 所在区域</span>
              </div>

              <div className="form-group">
                <label>Bucket <span className="required">*</span></label>
                <input
                  type="text"
                  name="bucket"
                  value={sopConfig.bucket}
                  onChange={(e) => handleSopConfigChange(index, e)}
                  placeholder="例如: my-sop-bucket"
                />
              </div>

              <div className="form-group">
                <label>Base Path <span className="required">*</span></label>
                <input
                  type="text"
                  name="basePath"
                  value={sopConfig.basePath}
                  onChange={(e) => handleSopConfigChange(index, e)}
                  placeholder="例如: /sop/docs/"
                />
                <span className="form-hint">SOP 文档在 Bucket 中的路径前缀</span>
              </div>

              <div className="form-group">
                <label>知识库描述</label>
                <textarea
                  name="description"
                  value={sopConfig.description}
                  onChange={(e) => handleSopConfigChange(index, e)}
                  placeholder="知识库的简要描述"
                  rows={2}
                />
              </div>
            </>
          )}

          {sopConfig.type === 'yunxiao' && (
            <>
              <div className="form-group">
                <label>组织 ID <span className="required">*</span></label>
                <input
                  type="text"
                  name="organizationId"
                  value={sopConfig.organizationId}
                  onChange={(e) => handleSopConfigChange(index, e)}
                  placeholder="云效组织 ID"
                />
              </div>

              <div className="form-group">
                <label>仓库 ID <span className="required">*</span></label>
                <input
                  type="text"
                  name="repositoryId"
                  value={sopConfig.repositoryId}
                  onChange={(e) => handleSopConfigChange(index, e)}
                  placeholder="代码仓库 ID"
                />
              </div>

              <div className="form-group">
                <label>分支名称 <span className="required">*</span></label>
                <input
                  type="text"
                  name="branchName"
                  value={sopConfig.branchName}
                  onChange={(e) => handleSopConfigChange(index, e)}
                  placeholder="例如: main"
                />
              </div>

              <div className="form-group">
                <label>Base Path <span className="required">*</span></label>
                <input
                  type="text"
                  name="basePath"
                  value={sopConfig.basePath}
                  onChange={(e) => handleSopConfigChange(index, e)}
                  placeholder="例如: /sop/docs/"
                />
                <span className="form-hint">SOP 文档在代码仓库中的路径前缀</span>
              </div>

              <div className="form-group">
                <label>访问 Token <span className="required">*</span></label>
                <input
                  type="password"
                  name="token"
                  value={sopConfig.token}
                  onChange={(e) => handleSopConfigChange(index, e)}
                  placeholder="云效访问令牌"
                />
              </div>

              <div className="form-group">
                <label>知识库描述</label>
                <textarea
                  name="description"
                  value={sopConfig.description}
                  onChange={(e) => handleSopConfigChange(index, e)}
                  placeholder="知识库的简要描述"
                  rows={2}
                />
              </div>
            </>
          )}

          {sopConfig.type === 'builtin' && (
            <div className="form-group">
              <label>知识库 ID <span className="required">*</span></label>
              <input
                type="text"
                name="id"
                value={sopConfig.id}
                onChange={(e) => handleSopConfigChange(index, e)}
                placeholder="内置知识库的 ID"
              />
            </div>
          )}
        </div>
      ))}

      <button type="button" className="add-btn" onClick={addSopConfig}>
        + 添加知识库
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
            ← 返回
          </button>
          <h2>创建 SOP 问答助手</h2>
        </div>
      </div>

      <div className="create-app-content">
        <div className="create-steps">
          <div className={`step ${step >= 1 ? 'active' : ''} ${step > 1 ? 'completed' : ''}`}>
            <span className="step-number">1</span>
            <span className="step-label">基本信息</span>
          </div>
          <div className="step-line"></div>
          <div className={`step ${step >= 2 ? 'active' : ''}`}>
            <span className="step-number">2</span>
            <span className="step-label">SOP 知识库配置</span>
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
                上一步
              </button>
            )}

            {step < 2 ? (
              <button
                type="button"
                className="btn-primary"
                onClick={() => setStep(step + 1)}
              >
                下一步
              </button>
            ) : (
              <button
                type="button"
                className="btn-primary"
                onClick={handleSubmit}
                disabled={creating}
              >
                {creating ? '创建中...' : '创建 SOP 问答助手'}
              </button>
            )}
          </div>
        </div>
      </div>
    </div>
  );
};

export default CreateEmployee;
