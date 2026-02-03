import { useState, useEffect } from 'react';
import { useDraggable, useDroppable, DndContext } from '@dnd-kit/core';
import type { DragEndEvent } from '@dnd-kit/core';
import { Plus, X, Play, Upload, Eye, Trash2, Edit2, Save, GitMerge } from 'lucide-react';
import type { CriteriaItem, SegmentDefinition, ChildSegmentRef, Condition, ConditionGroup as ConditionGroupType } from './types';
import { CRITERIA_CATALOG, CATEGORY_COLORS, SegmentType, Operator, Comparison } from './types';
import * as api from './api';
import './App.css';

// Draggable criteria box
function CriteriaBox({ item }: { item: CriteriaItem }) {
  const { attributes, listeners, setNodeRef, transform } = useDraggable({ id: item.id });
  const style = transform ? { transform: `translate(${transform.x}px, ${transform.y}px)` } : undefined;

  return (
    <div
      ref={setNodeRef}
      {...listeners}
      {...attributes}
      className="criteria-box"
      style={{ ...style, borderColor: CATEGORY_COLORS[item.category] }}
    >
      {item.label}
    </div>
  );
}

// Droppable builder area
function BuilderArea({ children }: { children: React.ReactNode }) {
  const { setNodeRef, isOver } = useDroppable({ id: 'builder' });
  return (
    <div ref={setNodeRef} className={`builder-area ${isOver ? 'drag-over' : ''}`}>
      {children}
    </div>
  );
}

// Droppable group area
function GroupDropZone({ groupId, children }: { groupId: string; children: React.ReactNode }) {
  const { setNodeRef, isOver } = useDroppable({ id: `group-${groupId}` });
  return (
    <div ref={setNodeRef} className={isOver ? 'group-drag-over' : ''}>
      {children}
    </div>
  );
}

interface SelectedCriteria {
  id: string;
  item: CriteriaItem;
  operator: number;
  value: string;
  logicOperator?: number;
}

interface CriteriaGroup {
  id: string;
  type: 'criteria' | 'group';
  logicOperator?: number;
  // For criteria type
  criteria?: SelectedCriteria;
  // For group type
  items?: CriteriaGroup[];
}

function App() {
  const [name, setName] = useState('');
  const [selected, setSelected] = useState<CriteriaGroup[]>([]);
  const [result, setResult] = useState<any>(null);
  const [loading, setLoading] = useState(false);
  const [segments, setSegments] = useState<any[]>([]);
  const [tab, setTab] = useState<'builder' | 'list' | 'upload' | 'composite'>('builder');
  const [uploadFile, setUploadFile] = useState<File | null>(null);
  const [uploadName, setUploadName] = useState('');
  const [uploadHeader, setUploadHeader] = useState('user_id');
  
  // Edit mode
  const [editingSegment, setEditingSegment] = useState<any>(null);
  
  // Composite segment state
  const [compositeName, setCompositeName] = useState('');
  const [selectedSegments, setSelectedSegments] = useState<(ChildSegmentRef & { operator?: number })[]>([]);
  
  // Profile field distinct values
  const [profileValues, setProfileValues] = useState<Record<string, string[]>>({
    platform: [],
    country: [],
    os: [],
    language: [],
    app_id: []
  });

  // Load profile field distinct values
  useEffect(() => {
    const loadProfileValues = async () => {
      try {
        const fields = ['platform', 'country', 'os', 'language', 'app_id'];
        const results: Record<string, string[]> = {};
        for (const field of fields) {
          const data = await api.getDistinctValues(field);
          results[field] = data.values || [];
        }
        setProfileValues(results);
      } catch (e) {
        console.error('Failed to load profile values:', e);
      }
    };
    loadProfileValues();
  }, []);

  const handleDragEnd = (event: DragEndEvent) => {
    const overId = event.over?.id;
    if (overId && event.active.id) {
      const item = CRITERIA_CATALOG.find(c => c.id === event.active.id);
      if (item) {
        const newCriteria: SelectedCriteria = {
          id: `${item.id}-${Date.now()}`,
          item,
          operator: item.defaultOperator,
          value: item.valueType === 'bool' ? 'true' : '',
          logicOperator: Operator.AND
        };
        
        const newGroup: CriteriaGroup = {
          id: `cg-${Date.now()}`,
          type: 'criteria',
          criteria: newCriteria,
          logicOperator: Operator.AND
        };
        
        // Check if dropping into a group
        if (typeof overId === 'string' && overId.startsWith('group-')) {
          const groupId = overId.replace('group-', '');
          addCriteriaToGroup(groupId, newGroup);
        } else if (overId === 'builder') {
          // Add to root level
          setSelected([...selected, newGroup]);
        }
      }
    }
  };

  const removeCriteria = (id: string) => {
    const removeFromGroup = (items: CriteriaGroup[]): CriteriaGroup[] => {
      return items.filter(item => {
        if (item.id === id) return false;
        if (item.type === 'group' && item.items) {
          item.items = removeFromGroup(item.items);
        }
        return true;
      });
    };
    setSelected(removeFromGroup(selected));
  };

  const updateCriteria = (id: string, field: 'operator' | 'value' | 'logicOperator', val: string | number) => {
    const updateInGroup = (items: CriteriaGroup[]): CriteriaGroup[] => {
      return items.map(item => {
        if (item.type === 'criteria' && item.criteria?.id === id) {
          return { ...item, criteria: { ...item.criteria, [field]: val } };
        } else if (item.type === 'group' && item.items) {
          return { ...item, items: updateInGroup(item.items) };
        }
        return item;
      });
    };
    setSelected(updateInGroup(selected));
  };

  const updateGroupOperator = (groupId: string, operator: number) => {
    const updateInGroup = (items: CriteriaGroup[]): CriteriaGroup[] => {
      return items.map(item => {
        if (item.id === groupId) {
          return { ...item, logicOperator: operator };
        } else if (item.type === 'group' && item.items) {
          return { ...item, items: updateInGroup(item.items) };
        }
        return item;
      });
    };
    setSelected(updateInGroup(selected));
  };

  const createGroup = () => {
    const newGroup: CriteriaGroup = {
      id: `grp-${Date.now()}`,
      type: 'group',
      items: [],
      logicOperator: Operator.AND
    };
    setSelected([...selected, newGroup]);
  };

  // Unused function - kept for future drag-and-drop feature
  // const addToGroup = (groupId: string, criteriaId: string) => {
  //   const moveToGroup = (items: CriteriaGroup[]): { items: CriteriaGroup[], moved?: CriteriaGroup } => {
  //     let movedItem: CriteriaGroup | undefined;
  //     const filtered = items.filter(item => {
  //       if (item.id === criteriaId) {
  //         movedItem = { ...item, logicOperator: Operator.AND };
  //         return false;
  //       }
  //       return true;
  //     });
  //     
  //     const updated = filtered.map(item => {
  //       if (item.id === groupId && item.type === 'group') {
  //         const newItems = [...(item.items || [])];
  //         if (movedItem) newItems.push(movedItem);
  //         return { ...item, items: newItems };
  //       } else if (item.type === 'group' && item.items) {
  //         const result = moveToGroup(item.items);
  //         if (result.moved && !movedItem) movedItem = result.moved;
  //         return { ...item, items: result.items };
  //       }
  //       return item;
  //     });
  //     
  //     return { items: updated, moved: movedItem };
  //   };
  //   
  //   const result = moveToGroup(selected);
  //   setSelected(result.items);
  // };

  const addCriteriaToGroup = (groupId: string, newItem: CriteriaGroup) => {
    const addToGroup = (items: CriteriaGroup[]): CriteriaGroup[] => {
      return items.map(item => {
        if (item.id === groupId && item.type === 'group') {
          return { ...item, items: [...(item.items || []), newItem] };
        } else if (item.type === 'group' && item.items) {
          return { ...item, items: addToGroup(item.items) };
        }
        return item;
      });
    };
    setSelected(addToGroup(selected));
  };

  const buildDefinition = (): SegmentDefinition => {
    if (selected.length === 0) {
      return { type: SegmentType.DYNAMIC, userConditions: { operator: Operator.AND, conditions: [] } };
    }

    const criteriaToCondition = (crit: SelectedCriteria): Condition => {
      let value: any = {};
      if (crit.item.valueType === 'bool') {
        value = { boolValue: crit.value === 'true' };
      } else if (crit.item.valueType === 'number') {
        value = { doubleValue: parseFloat(crit.value) || 0 };
      } else if (crit.item.valueType === 'list' || crit.operator === Comparison.IN) {
        value = { stringList: { values: crit.value.split(',').map(v => v.trim()) } };
      } else {
        value = { stringValue: crit.value };
      }
      return { field: crit.item.field, operator: crit.operator, value };
    };

    const groupToConditionGroup = (items: CriteriaGroup[]): { conditions: Condition[], groups: ConditionGroupType[] } => {
      const conditions: Condition[] = [];
      const groups: ConditionGroupType[] = [];

      // Determine the operator for this level by checking logicOperators
      const hasOr = items.some((item, idx) => idx > 0 && item.logicOperator === Operator.OR);
      const operator = hasOr ? Operator.OR : Operator.AND;

      if (operator === Operator.AND) {
        // All AND: flatten everything at this level
        items.forEach(item => {
          if (item.type === 'criteria' && item.criteria) {
            conditions.push(criteriaToCondition(item.criteria));
          } else if (item.type === 'group' && item.items && item.items.length > 0) {
            const subGroup = groupToConditionGroup(item.items);
            // Determine sub-group operator
            const subHasOr = item.items.some((it, idx) => idx > 0 && it.logicOperator === Operator.OR);
            groups.push({
              operator: subHasOr ? Operator.OR : Operator.AND,
              conditions: subGroup.conditions,
              groups: subGroup.groups.length > 0 ? subGroup.groups : undefined
            });
          }
        });
      } else {
        // Mixed AND/OR: split by OR
        let currentAndItems: CriteriaGroup[] = [];
        
        items.forEach((item, idx) => {
          if (idx > 0 && item.logicOperator === Operator.OR) {
            if (currentAndItems.length > 0) {
              const andGroup = groupToConditionGroup(currentAndItems);
              groups.push({
                operator: Operator.AND,
                conditions: andGroup.conditions,
                groups: andGroup.groups.length > 0 ? andGroup.groups : undefined
              });
            }
            currentAndItems = [item];
          } else {
            currentAndItems.push(item);
          }
        });
        
        if (currentAndItems.length > 0) {
          const andGroup = groupToConditionGroup(currentAndItems);
          groups.push({
            operator: Operator.AND,
            conditions: andGroup.conditions,
            groups: andGroup.groups.length > 0 ? andGroup.groups : undefined
          });
        }
      }

      return { conditions, groups };
    };

    const result = groupToConditionGroup(selected);
    const hasOr = selected.some((item, idx) => idx > 0 && item.logicOperator === Operator.OR);
    
    return {
      type: SegmentType.DYNAMIC,
      userConditions: {
        operator: hasOr ? Operator.OR : Operator.AND,
        conditions: result.conditions,
        groups: result.groups.length > 0 ? result.groups : undefined
      }
    };
  };

  const handlePreview = async () => {
    setLoading(true);
    try {
      const res = await api.previewSegment(buildDefinition());
      setResult(res);
    } catch (e: any) {
      setResult({ error: e.message });
    }
    setLoading(false);
  };

  const handleCreate = async () => {
    if (!name) return alert('Enter segment name');
    setLoading(true);
    try {
      const res = await api.createSegment({ name, definition: buildDefinition() });
      // Auto-evaluate the newly created segment
      if (res.segment?.id) {
        try {
          const evalRes = await api.evaluateSegment(res.segment.id);
          setResult({ ...res, evaluation: evalRes });
        } catch (evalErr) {
          console.error('Auto-evaluation failed:', evalErr);
          setResult(res);
        }
      } else {
        setResult(res);
      }
      setName('');
      setSelected([]);
      await loadSegments();
    } catch (e: any) {
      setResult({ error: e.message });
    }
    setLoading(false);
  };

  const loadSegments = async () => {
    try {
      const res = await api.listSegments();
      setSegments(res.segments || []);
    } catch (e) {
      console.error(e);
    }
  };

  const handleEvaluate = async (id: string) => {
    setLoading(true);
    try {
      const res = await api.evaluateSegment(id);
      setResult(res);
      // Reload segments list to get updated count from database
      await loadSegments();
    } catch (e: any) {
      setResult({ error: e.message });
    }
    setLoading(false);
  };

  const handleDelete = async (id: string) => {
    if (!confirm('Delete this segment?')) return;
    try {
      await api.deleteSegment(id);
      loadSegments();
    } catch (e: any) {
      alert(e.message);
    }
  };

  const handleEdit = async (seg: any) => {
    setEditingSegment(seg);
    setName(seg.name);
    // Load criteria from definition - simplified loading
    if (seg.definition?.userConditions?.conditions) {
      const loadedGroups: CriteriaGroup[] = seg.definition.userConditions.conditions.map((cond: any, idx: number) => {
        const item = CRITERIA_CATALOG.find(c => c.field === cond.field) || {
          id: cond.field,
          label: cond.field,
          field: cond.field,
          category: 'profile' as const,
          defaultOperator: cond.operator,
          valueType: 'string' as const
        };
        let value = '';
        if (cond.value?.boolValue !== undefined) value = String(cond.value.boolValue);
        else if (cond.value?.stringValue) value = cond.value.stringValue;
        else if (cond.value?.doubleValue !== undefined) value = String(cond.value.doubleValue);
        else if (cond.value?.intValue !== undefined) value = String(cond.value.intValue);
        else if (cond.value?.stringList?.values) value = cond.value.stringList.values.join(', ');
        
        const criteria: SelectedCriteria = {
          id: `${cond.field}-${idx}`,
          item,
          operator: cond.operator,
          value,
          logicOperator: Operator.AND
        };
        
        return {
          id: `cg-${idx}`,
          type: 'criteria' as const,
          criteria,
          logicOperator: idx === 0 ? undefined : Operator.AND
        };
      });
      setSelected(loadedGroups);
    }
    setTab('builder');
  };

  const handleUpdate = async () => {
    if (!editingSegment) return;
    setLoading(true);
    try {
      const res = await api.updateSegment(editingSegment.id, { 
        name, 
        definition: buildDefinition() 
      });
      setResult(res);
      setEditingSegment(null);
      setName('');
      setSelected([]);
      loadSegments();
    } catch (e: any) {
      setResult({ error: e.message });
    }
    setLoading(false);
  };

  const cancelEdit = () => {
    setEditingSegment(null);
    setName('');
    setSelected([]);
  };

  const handleUpload = async () => {
    if (!uploadFile || !uploadName) return alert('Fill all fields');
    setLoading(true);
    try {
      const res = await api.uploadStaticSegment(uploadName, uploadFile, uploadHeader);
      setResult(res);
      setUploadFile(null);
      setUploadName('');
      loadSegments();
    } catch (e: any) {
      setResult({ error: e.message });
    }
    setLoading(false);
  };

  // Composite segment handlers
  const addSegmentToComposite = (segmentId: string) => {
    if (selectedSegments.find(s => s.segmentId === segmentId)) return;
    setSelectedSegments([...selectedSegments, { segmentId, negated: false, operator: Operator.AND }]);
  };

  const removeSegmentFromComposite = (segmentId: string) => {
    setSelectedSegments(selectedSegments.filter(s => s.segmentId !== segmentId));
  };

  const toggleNegated = (segmentId: string) => {
    setSelectedSegments(selectedSegments.map(s => 
      s.segmentId === segmentId ? { ...s, negated: !s.negated } : s
    ));
  };

  const updateSegmentOperator = (segmentId: string, operator: number) => {
    setSelectedSegments(selectedSegments.map(s => 
      s.segmentId === segmentId ? { ...s, operator } : s
    ));
  };

  const buildCompositeDefinition = (): SegmentDefinition => {
    if (selectedSegments.length === 0) {
      return { type: SegmentType.COMPOSITE, childSegments: [], childLogic: Operator.AND };
    }

    // Check if we have mixed operators
    const hasOr = selectedSegments.some((s, idx) => idx > 0 && s.operator === Operator.OR);
    
    if (!hasOr) {
      // All AND
      return {
        type: SegmentType.COMPOSITE,
        childSegments: selectedSegments.map(s => ({ segmentId: s.segmentId, negated: s.negated })),
        childLogic: Operator.AND
      };
    } else {
      // For now, use the last operator as the dominant one
      // A more complex implementation would need nested composite segments
      const dominantOp = selectedSegments[selectedSegments.length - 1].operator || Operator.AND;
      return {
        type: SegmentType.COMPOSITE,
        childSegments: selectedSegments.map(s => ({ segmentId: s.segmentId, negated: s.negated })),
        childLogic: dominantOp
      };
    }
  };

  const handleCreateComposite = async () => {
    if (!compositeName) return alert('Enter segment name');
    if (selectedSegments.length < 2) return alert('Select at least 2 segments');
    setLoading(true);
    try {
      const res = await api.createSegment({ name: compositeName, definition: buildCompositeDefinition() });
      // Auto-evaluate the newly created composite segment
      if (res.segment?.id) {
        try {
          const evalRes = await api.evaluateSegment(res.segment.id);
          setResult({ ...res, evaluation: evalRes });
        } catch (evalErr) {
          console.error('Auto-evaluation failed:', evalErr);
          setResult(res);
        }
      } else {
        setResult(res);
      }
      setCompositeName('');
      setSelectedSegments([]);
      await loadSegments();
    } catch (e: any) {
      setResult({ error: e.message });
    }
    setLoading(false);
  };

  const handlePreviewComposite = async () => {
    if (selectedSegments.length < 2) return alert('Select at least 2 segments');
    setLoading(true);
    try {
      const res = await api.previewSegment(buildCompositeDefinition());
      setResult(res);
    } catch (e: any) {
      setResult({ error: e.message });
    }
    setLoading(false);
  };

  const categories = ['activity', 'payment', 'churned', 'profile', 'revenue', 'vip'];

  const getSegmentTypeLabel = (definition: any): string => {
    if (!definition?.type) return 'Unknown';
    const type = definition.type;
    // Handle both numeric and string enum values
    if (type === 1 || type === 'SEGMENT_TYPE_STATIC') return 'Static';
    if (type === 3 || type === 'SEGMENT_TYPE_COMPOSITE') return 'Composite';
    if (type === 2 || type === 'SEGMENT_TYPE_DYNAMIC') return 'Dynamic';
    return 'Dynamic';
  };

  const renderCriteriaGroup = (group: CriteriaGroup, index: number, depth: number = 0): React.ReactNode => {
    if (group.type === 'criteria' && group.criteria) {
      const s = group.criteria;
      return (
        <div key={group.id} className="selected-criteria" style={{ marginLeft: `${depth * 20}px` }}>
          {index > 0 && (
            <select 
              className="logic-selector" 
              value={group.logicOperator || Operator.AND} 
              onChange={e => updateGroupOperator(group.id, Number(e.target.value))}
            >
              <option value={Operator.AND}>AND</option>
              <option value={Operator.OR}>OR</option>
            </select>
          )}
          <span className="field" style={{ borderColor: CATEGORY_COLORS[s.item.category] }}>
            {s.item.label}
          </span>
          <select value={s.operator} onChange={e => updateCriteria(s.id, 'operator', Number(e.target.value))}>
            <option value={1}>=</option>
            <option value={2}>≠</option>
            <option value={3}>&gt;</option>
            <option value={4}>≥</option>
            <option value={5}>&lt;</option>
            <option value={6}>≤</option>
            <option value={7}>IN</option>
            <option value={8}>NOT IN</option>
          </select>
          {s.item.valueType === 'bool' ? (
            <select value={s.value} onChange={e => updateCriteria(s.id, 'value', e.target.value)}>
              <option value="true">Yes</option>
              <option value="false">No</option>
            </select>
          ) : s.item.category === 'profile' && profileValues[s.item.field]?.length > 0 ? (
            (s.operator === 7 || s.operator === 8) ? (
              <select 
                value={s.value.split(',').map(v => v.trim()).filter(Boolean)} 
                onChange={e => {
                  const selected = Array.from(e.target.selectedOptions).map(opt => opt.value);
                  updateCriteria(s.id, 'value', selected.join(', '));
                }}
                multiple
                size={Math.min(5, profileValues[s.item.field].length)}
                style={{ minWidth: '150px' }}
              >
                {profileValues[s.item.field].map(val => (
                  <option key={val} value={val}>{val}</option>
                ))}
              </select>
            ) : (
              <select 
                value={s.value} 
                onChange={e => updateCriteria(s.id, 'value', e.target.value)}
              >
                <option value="">-- Select --</option>
                {profileValues[s.item.field].map(val => (
                  <option key={val} value={val}>{val}</option>
                ))}
              </select>
            )
          ) : (
            <input
              type={s.item.valueType === 'number' ? 'number' : 'text'}
              placeholder={s.item.valueType === 'list' ? 'val1, val2' : 'value'}
              value={s.value}
              onChange={e => updateCriteria(s.id, 'value', e.target.value)}
            />
          )}
          <button className="remove" onClick={() => removeCriteria(group.id)}><X size={16} /></button>
        </div>
      );
    } else if (group.type === 'group' && group.items) {
      return (
        <GroupDropZone key={group.id} groupId={group.id}>
          <div className="criteria-group" style={{ marginLeft: `${depth * 20}px` }}>
            <div className="group-header">
              {index > 0 && (
                <select 
                  className="logic-selector" 
                  value={group.logicOperator || Operator.AND} 
                  onChange={e => updateGroupOperator(group.id, Number(e.target.value))}
                >
                  <option value={Operator.AND}>AND</option>
                  <option value={Operator.OR}>OR</option>
                </select>
              )}
              <span className="group-label">( Group</span>
              <button className="remove" onClick={() => removeCriteria(group.id)}><X size={16} /></button>
            </div>
            <div className="group-content">
              {group.items.map((item, idx) => renderCriteriaGroup(item, idx, depth + 1))}
              {group.items.length === 0 && (
                <p className="placeholder" style={{ margin: '0.5rem' }}>Empty group - drag criteria here</p>
              )}
            </div>
            <div className="group-footer" style={{ marginLeft: `${depth * 20}px` }}>)</div>
          </div>
        </GroupDropZone>
      );
    }
    return null;
  };

  return (
    <DndContext onDragEnd={handleDragEnd}>
      <div className="app">
        <header>
          <h1>Segment Builder</h1>
          <nav>
            <button className={tab === 'builder' ? 'active' : ''} onClick={() => setTab('builder')}>Builder</button>
            <button className={tab === 'list' ? 'active' : ''} onClick={() => { setTab('list'); loadSegments(); }}>Segments</button>
            <button className={tab === 'composite' ? 'active' : ''} onClick={() => { setTab('composite'); loadSegments(); }}>Composite</button>
            <button className={tab === 'upload' ? 'active' : ''} onClick={() => setTab('upload')}>Upload</button>
          </nav>
        </header>

        {tab === 'builder' && (
          <div className="content">
            <aside className="criteria-panel">
              <h3>Criteria</h3>
              {categories.map(cat => (
                <div key={cat} className="category">
                  <h4 style={{ color: CATEGORY_COLORS[cat] }}>{cat.toUpperCase()}</h4>
                  <div className="criteria-list">
                    {CRITERIA_CATALOG.filter(c => c.category === cat).map(item => (
                      <CriteriaBox key={item.id} item={item} />
                    ))}
                  </div>
                </div>
              ))}
            </aside>

            <main>
              <div className="builder-header">
                <input
                  type="text"
                  placeholder="Segment name"
                  value={name}
                  onChange={e => setName(e.target.value)}
                />
                {editingSegment && (
                  <button className="cancel-btn" onClick={cancelEdit}>Cancel Edit</button>
                )}
              </div>

              <BuilderArea>
                {selected.length === 0 ? (
                  <p className="placeholder">Drag criteria here</p>
                ) : (
                  selected.map((group, i) => renderCriteriaGroup(group, i))
                )}
              </BuilderArea>
              
              <div className="builder-controls">
                <button className="add-group-btn" onClick={createGroup}>
                  <Plus size={16} /> Add Group
                </button>
              </div>

              <div className="actions">
                <button onClick={handlePreview} disabled={loading || selected.length === 0}>
                  <Eye size={16} /> Preview
                </button>
                {editingSegment ? (
                  <button onClick={handleUpdate} disabled={loading || selected.length === 0 || !name}>
                    <Save size={16} /> Update
                  </button>
                ) : (
                  <button onClick={handleCreate} disabled={loading || selected.length === 0 || !name}>
                    <Plus size={16} /> Create
                  </button>
                )}
              </div>

              {result && (
                <div className="result">
                  <h4>Result</h4>
                  <pre>{JSON.stringify(result, null, 2)}</pre>
                </div>
              )}
            </main>
          </div>
        )}

        {tab === 'list' && (
          <div className="segment-list">
            <h3>Saved Segments</h3>
            {segments.length === 0 ? (
              <p>No segments yet</p>
            ) : (
              <table>
                <thead>
                  <tr><th>Name</th><th>Type</th><th>Count</th><th>Actions</th></tr>
                </thead>
                <tbody>
                  {segments.map((seg: any) => (
                    <tr key={seg.id}>
                      <td>{seg.name}</td>
                      <td>{getSegmentTypeLabel(seg.definition)}</td>
                      <td>{seg.cachedCount || '-'}</td>
                      <td>
                        <button onClick={() => handleEvaluate(seg.id)} title="Evaluate"><Play size={14} /></button>
                        <button onClick={() => handleEdit(seg)} title="Edit"><Edit2 size={14} /></button>
                        <button onClick={() => handleDelete(seg.id)} title="Delete"><Trash2 size={14} /></button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
            {result && (
              <div className="result">
                <h4>Result</h4>
                <pre>{JSON.stringify(result, null, 2)}</pre>
              </div>
            )}
          </div>
        )}

        {tab === 'upload' && (
          <div className="upload-panel">
            <h3>Upload Static Segment</h3>
            <input
              type="text"
              placeholder="Segment name"
              value={uploadName}
              onChange={e => setUploadName(e.target.value)}
            />
            <input
              type="text"
              placeholder="Column header (default: user_id)"
              value={uploadHeader}
              onChange={e => setUploadHeader(e.target.value)}
            />
            <input
              type="file"
              accept=".csv,.xlsx"
              onChange={e => setUploadFile(e.target.files?.[0] || null)}
            />
            <button onClick={handleUpload} disabled={loading}>
              <Upload size={16} /> Upload
            </button>
            {result && (
              <div className="result">
                <pre>{JSON.stringify(result, null, 2)}</pre>
              </div>
            )}
          </div>
        )}

        {tab === 'composite' && (
          <div className="composite-panel">
            <h3><GitMerge size={20} /> Create Composite Segment</h3>
            <p className="hint">Combine existing segments using AND/OR logic</p>
            
            <div className="composite-header">
              <input
                type="text"
                placeholder="Composite segment name"
                value={compositeName}
                onChange={e => setCompositeName(e.target.value)}
              />
            </div>

            <div className="composite-builder">
              <div className="available-segments">
                <h4>Available Segments</h4>
                {segments.filter(s => !selectedSegments.find(ss => ss.segmentId === s.id)).map((seg: any) => (
                  <div 
                    key={seg.id} 
                    className="segment-chip available"
                    onClick={() => addSegmentToComposite(seg.id)}
                  >
                    <Plus size={14} />
                    <span>{seg.name}</span>
                    <span className="type-badge">
                      {seg.definition?.type === 1 ? 'S' : seg.definition?.type === 3 ? 'C' : 'D'}
                    </span>
                  </div>
                ))}
                {segments.length === 0 && <p className="empty">No segments available</p>}
              </div>

              <div className="selected-segments">
                <h4>Selected Segments ({selectedSegments.length})</h4>
                {selectedSegments.length === 0 ? (
                  <p className="empty">Click segments to add</p>
                ) : (
                  selectedSegments.map((ref, idx) => {
                    const seg = segments.find((s: any) => s.id === ref.segmentId);
                    return (
                      <div key={ref.segmentId} className="segment-chip selected">
                        {idx > 0 && (
                          <select
                            className="logic-selector"
                            value={ref.operator || Operator.AND}
                            onChange={e => updateSegmentOperator(ref.segmentId, Number(e.target.value))}
                          >
                            <option value={Operator.AND}>AND</option>
                            <option value={Operator.OR}>OR</option>
                          </select>
                        )}
                        <button 
                          className={`negate-btn ${ref.negated ? 'active' : ''}`}
                          onClick={() => toggleNegated(ref.segmentId)}
                          title="Toggle NOT"
                        >
                          {ref.negated ? 'NOT' : ''}
                        </button>
                        <span className="name">{seg?.name || ref.segmentId}</span>
                        <button 
                          className="remove-btn"
                          onClick={() => removeSegmentFromComposite(ref.segmentId)}
                        >
                          <X size={14} />
                        </button>
                      </div>
                    );
                  })
                )}
              </div>
            </div>

            <div className="composite-actions">
              <button onClick={handlePreviewComposite} disabled={loading || selectedSegments.length < 2}>
                <Eye size={16} /> Preview
              </button>
              <button onClick={handleCreateComposite} disabled={loading || selectedSegments.length < 2 || !compositeName}>
                <Plus size={16} /> Create Composite
              </button>
            </div>

            {result && (
              <div className="result">
                <h4>Result</h4>
                <pre>{JSON.stringify(result, null, 2)}</pre>
              </div>
            )}
          </div>
        )}
      </div>
    </DndContext>
  );
}

export default App;
