/**
 * EmployeeSettings Component
 * Full page settings view to display and edit employee configuration with tabs
 */
import React, { useState, useEffect } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { getEmployee, updateEmployee, getAccountId } from '../services/api';
import { useAuth } from '../contexts/AuthContext';

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

  const handleSave = async () => {
    setError(null);

    // 验证必填字段
    if (!formData.displayName.trim()) {
      setError('请输入显示名称');
      return;
    }
    if (!formData.defaultRule.trim()) {
      setError('请输入角色定义');
      return;
    }

    setSaving(true);

    try {
      // 构建所有 SOP Knowledges
      const sopKnowledges = sopConfigs.map((config, index) => {
        try {
          return buildSopKnowledge(config);
        } catch (err) {
          throw new Error(`知识库 ${index + 1}: ${err.message}`);
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
      oss: { bg: '#f0f5ff', accent: '#2f54eb', icon: '☁️', label: 'OSS 对象存储' },
      yunxiao: { bg: '#f6ffed', accent: '#52c41a', icon: '🌿', label: '云效代码库' },
      builtin: { bg: '#fff7e6', accent: '#fa8c16', icon: '📚', label: '内置知识库' }
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
          <strong style={{ color: '#333', fontSize: '15px' }}>知识库 {index + 1}</strong>
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
                <label>Region</label>
                <span className="info-value code">{knowledge.region || '-'}</span>
              </div>
              <div className="info-row">
                <label>Bucket</label>
                <span className="info-value code">{knowledge.bucket || '-'}</span>
              </div>
              <div className="info-row">
                <label>Base Path</label>
                <span className="info-value code">{knowledge.basePath || '-'}</span>
              </div>
              {knowledge.description && (
                <div className="info-row">
                  <label>知识库描述</label>
                  <span className="info-value">{knowledge.description}</span>
                </div>
              )}
            </>
          )}
          
          {type === 'yunxiao' && (
            <>
              <div className="info-row">
                <label>组织 ID</label>
                <span className="info-value code">{knowledge.organizationId || '-'}</span>
              </div>
              <div className="info-row">
                <label>仓库 ID</label>
                <span className="info-value code">{knowledge.repositoryId || '-'}</span>
              </div>
              <div className="info-row">
                <label>分支名称</label>
                <span className="info-value code">{knowledge.branchName || '-'}</span>
              </div>
              <div className="info-row">
                <label>Base Path</label>
                <span className="info-value code">{knowledge.basePath || '-'}</span>
              </div>
              {knowledge.description && (
                <div className="info-row">
                  <label>知识库描述</label>
                  <span className="info-value">{knowledge.description}</span>
                </div>
              )}
            </>
          )}
          
          {type === 'builtin' && (
            <div className="info-row">
              <label>知识库 ID</label>
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
            value={knowledge.type}
            onChange={(e) => handleSopConfigChange(index, e)}
          >
            <option value="oss">OSS 对象存储</option>
            <option value="yunxiao">云效代码库</option>
            <option value="builtin">内置知识库</option>
          </select>
        </div>

        {knowledge.type === 'oss' && (
          <>
            <div className="form-group">
              <label>Region <span className="required">*</span></label>
              <input
                type="text"
                name="region"
                value={knowledge.region}
                onChange={(e) => handleSopConfigChange(index, e)}
                placeholder="例如: cn-hangzhou"
              />
            </div>

            <div className="form-group">
              <label>Bucket <span className="required">*</span></label>
              <input
                type="text"
                name="bucket"
                value={knowledge.bucket}
                onChange={(e) => handleSopConfigChange(index, e)}
                placeholder="例如: my-sop-bucket"
              />
            </div>

            <div className="form-group">
              <label>Base Path <span className="required">*</span></label>
              <input
                type="text"
                name="basePath"
                value={knowledge.basePath}
                onChange={(e) => handleSopConfigChange(index, e)}
                placeholder="例如: /sop/docs/"
              />
            </div>

            <div className="form-group">
              <label>知识库描述</label>
              <textarea
                name="description"
                value={knowledge.description}
                onChange={(e) => handleSopConfigChange(index, e)}
                placeholder="知识库的简要描述"
                rows={2}
              />
            </div>
          </>
        )}

        {knowledge.type === 'yunxiao' && (
          <>
            <div className="form-group">
              <label>组织 ID <span className="required">*</span></label>
              <input
                type="text"
                name="organizationId"
                value={knowledge.organizationId}
                onChange={(e) => handleSopConfigChange(index, e)}
                placeholder="云效组织 ID"
              />
            </div>

            <div className="form-group">
              <label>仓库 ID <span className="required">*</span></label>
              <input
                type="text"
                name="repositoryId"
                value={knowledge.repositoryId}
                onChange={(e) => handleSopConfigChange(index, e)}
                placeholder="代码仓库 ID"
              />
            </div>

            <div className="form-group">
              <label>分支名称 <span className="required">*</span></label>
              <input
                type="text"
                name="branchName"
                value={knowledge.branchName}
                onChange={(e) => handleSopConfigChange(index, e)}
                placeholder="例如: main"
              />
            </div>

            <div className="form-group">
              <label>Base Path <span className="required">*</span></label>
              <input
                type="text"
                name="basePath"
                value={knowledge.basePath}
                onChange={(e) => handleSopConfigChange(index, e)}
                placeholder="例如: /sop/docs/"
              />
            </div>

            <div className="form-group">
              <label>访问 Token <span className="required">*</span></label>
              <input
                type="password"
                name="token"
                value={knowledge.token}
                onChange={(e) => handleSopConfigChange(index, e)}
                placeholder="云效访问令牌"
              />
            </div>

            <div className="form-group">
              <label>知识库描述</label>
              <textarea
                name="description"
                value={knowledge.description}
                onChange={(e) => handleSopConfigChange(index, e)}
                placeholder="知识库的简要描述"
                rows={2}
              />
            </div>
          </>
        )}

        {knowledge.type === 'builtin' && (
          <div className="form-group">
            <label>知识库 ID <span className="required">*</span></label>
            <input
              type="text"
              name="id"
              value={knowledge.id}
              onChange={(e) => handleSopConfigChange(index, e)}
              placeholder="内置知识库的 ID"
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
              <label>员工名称</label>
              <input
                type="text"
                value={config.name}
                disabled
                style={{ backgroundColor: '#f5f5f5', cursor: 'not-allowed' }}
              />
              <span className="form-hint">员工名称不可修改</span>
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
            </div>

            <div className="form-group">
              <label>描述</label>
              <input
                type="text"
                name="description"
                value={formData.description}
                onChange={handleInputChange}
                placeholder="简要描述该员工的功能"
              />
            </div>

            <div className="form-group">
              <label>Role ARN</label>
              <input
                type="text"
                value={config.roleArn || ''}
                disabled
                style={{ backgroundColor: '#f5f5f5', cursor: 'not-allowed', fontFamily: 'monospace' }}
              />
              <span className="form-hint">Role ARN 不可修改</span>
            </div>

            <div className="form-group">
              <label>角色定义 <span className="required">*</span></label>
              <textarea
                name="defaultRule"
                value={formData.defaultRule}
                onChange={handleInputChange}
                placeholder="例如：你是一个专业的 OSS 日志分析工程师，擅长排查日志相关问题..."
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
              <h4>基本信息</h4>
            </div>
            <div className="info-card-content">
              <div className="info-row">
                <label>员工名称</label>
                <span className="info-value code">{config.name}</span>
              </div>
              <div className="info-row">
                <label>显示名称</label>
                <span className="info-value">{config.displayName}</span>
              </div>
              {config.description && (
                <div className="info-row">
                  <label>描述</label>
                  <span className="info-value">{config.description}</span>
                </div>
              )}
              {config.roleArn && (
                <div className="info-row">
                  <label>Role ARN</label>
                  <span className="info-value code">{config.roleArn}</span>
                </div>
              )}
              {config.employeeType && (
                <div className="info-row">
                  <label>员工类型</label>
                  <span className="info-value">{config.employeeType}</span>
                </div>
              )}
              {config.regionId && (
                <div className="info-row">
                  <label>Region ID</label>
                  <span className="info-value code">{config.regionId}</span>
                </div>
              )}
              {config.createTime && (
                <div className="info-row">
                  <label>创建时间</label>
                  <span className="info-value">{formatTime(config.createTime)}</span>
                </div>
              )}
              {config.updateTime && (
                <div className="info-row">
                  <label>更新时间</label>
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
                <h4>角色定义</h4>
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
              + 添加知识库
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
            <p>暂无配置的 SOP 知识库</p>
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
              已配置的知识库
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
              ← 返回
            </button>
            <h2>⚙️ 员工设置</h2>
          </div>
        </div>
        <div className="settings-page-content">
          <div className="settings-page-loading">
            <div className="loading-spinner"></div>
            <p>加载配置中...</p>
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
              ← 返回
            </button>
            <h2>⚙️ 员工设置</h2>
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
            ← 返回
          </button>
          <h2>⚙️ {config.displayName || config.name} - 设置</h2>
        </div>
        <div className="header-actions">
          {isAdmin && (
            !editMode ? (
              <button className="btn-primary" onClick={() => setEditMode(true)}>
                ✏️ 编辑
              </button>
            ) : (
              <>
                <button 
                  className="btn-secondary" 
                  onClick={handleCancel}
                  disabled={saving}
                >
                  取消
                </button>
                <button 
                  className="btn-primary" 
                  onClick={handleSave}
                  disabled={saving}
                >
                  {saving ? '保存中...' : '💾 保存'}
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
          📋 基本信息
        </button>
        <button 
          className={`page-tab ${activeTab === 'knowledges' ? 'active' : ''}`}
          onClick={() => setActiveTab('knowledges')}
        >
          📚 SOP 知识库配置
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
    return date.toLocaleString('zh-CN', {
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
