import { useState } from 'react';
import { useDraggable, useDroppable, DndContext } from '@dnd-kit/core';
import type { DragEndEvent } from '@dnd-kit/core';
import { Plus, X, Play, Upload, Eye, Trash2 } from 'lucide-react';
import type { CriteriaItem, SegmentDefinition } from './types';
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

interface SelectedCriteria {
  id: string;
  item: CriteriaItem;
  operator: number;
  value: string;
}

function App() {
  const [name, setName] = useState('');
  const [selected, setSelected] = useState<SelectedCriteria[]>([]);
  const [logic, setLogic] = useState<number>(Operator.AND);
  const [result, setResult] = useState<any>(null);
  const [loading, setLoading] = useState(false);
  const [segments, setSegments] = useState<any[]>([]);
  const [tab, setTab] = useState<'builder' | 'list' | 'upload'>('builder');
  const [uploadFile, setUploadFile] = useState<File | null>(null);
  const [uploadName, setUploadName] = useState('');
  const [uploadHeader, setUploadHeader] = useState('user_id');

  const handleDragEnd = (event: DragEndEvent) => {
    if (event.over?.id === 'builder' && event.active.id) {
      const item = CRITERIA_CATALOG.find(c => c.id === event.active.id);
      if (item && !selected.find(s => s.item.field === item.field)) {
        setSelected([...selected, {
          id: `${item.id}-${Date.now()}`,
          item,
          operator: item.defaultOperator,
          value: item.valueType === 'bool' ? 'true' : ''
        }]);
      }
    }
  };

  const removeCriteria = (id: string) => {
    setSelected(selected.filter(s => s.id !== id));
  };

  const updateCriteria = (id: string, field: 'operator' | 'value', val: string | number) => {
    setSelected(selected.map(s => s.id === id ? { ...s, [field]: val } : s));
  };

  const buildDefinition = (): SegmentDefinition => {
    const conditions: Condition[] = selected.map(s => {
      let value: any = {};
      if (s.item.valueType === 'bool') {
        value = { boolValue: s.value === 'true' };
      } else if (s.item.valueType === 'number') {
        value = { doubleValue: parseFloat(s.value) || 0 };
      } else if (s.item.valueType === 'list' || s.operator === Comparison.IN) {
        value = { stringList: { values: s.value.split(',').map(v => v.trim()) } };
      } else {
        value = { stringValue: s.value };
      }
      return { field: s.item.field, operator: s.operator, value };
    });

    return {
      type: SegmentType.DYNAMIC,
      userConditions: { operator: logic, conditions }
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
      setResult(res);
      setName('');
      loadSegments();
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

  const categories = ['activity', 'payment', 'churned', 'profile', 'revenue'];

  return (
    <DndContext onDragEnd={handleDragEnd}>
      <div className="app">
        <header>
          <h1>Segment Builder</h1>
          <nav>
            <button className={tab === 'builder' ? 'active' : ''} onClick={() => setTab('builder')}>Builder</button>
            <button className={tab === 'list' ? 'active' : ''} onClick={() => { setTab('list'); loadSegments(); }}>Segments</button>
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
                <select value={logic} onChange={e => setLogic(Number(e.target.value))}>
                  <option value={Operator.AND}>AND</option>
                  <option value={Operator.OR}>OR</option>
                </select>
              </div>

              <BuilderArea>
                {selected.length === 0 ? (
                  <p className="placeholder">Drag criteria here</p>
                ) : (
                  selected.map((s, i) => (
                    <div key={s.id} className="selected-criteria">
                      {i > 0 && <span className="logic-badge">{logic === Operator.AND ? 'AND' : 'OR'}</span>}
                      <span className="field" style={{ borderColor: CATEGORY_COLORS[s.item.category] }}>
                        {s.item.label}
                      </span>
                      <select value={s.operator} onChange={e => updateCriteria(s.id, 'operator', Number(e.target.value))}>
                        <option value={1}>= (EQ)</option>
                        <option value={2}>≠ (NEQ)</option>
                        <option value={3}>&gt; (GT)</option>
                        <option value={4}>≥ (GTE)</option>
                        <option value={5}>&lt; (LT)</option>
                        <option value={6}>≤ (LTE)</option>
                        <option value={7}>IN</option>
                        <option value={8}>NOT IN</option>
                      </select>
                      {s.item.valueType === 'bool' ? (
                        <select value={s.value} onChange={e => updateCriteria(s.id, 'value', e.target.value)}>
                          <option value="true">Yes</option>
                          <option value="false">No</option>
                        </select>
                      ) : (
                        <input
                          type={s.item.valueType === 'number' ? 'number' : 'text'}
                          placeholder={s.item.valueType === 'list' ? 'val1, val2' : 'value'}
                          value={s.value}
                          onChange={e => updateCriteria(s.id, 'value', e.target.value)}
                        />
                      )}
                      <button className="remove" onClick={() => removeCriteria(s.id)}><X size={16} /></button>
                    </div>
                  ))
                )}
              </BuilderArea>

              <div className="actions">
                <button onClick={handlePreview} disabled={loading || selected.length === 0}>
                  <Eye size={16} /> Preview
                </button>
                <button onClick={handleCreate} disabled={loading || selected.length === 0 || !name}>
                  <Plus size={16} /> Create
                </button>
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
                      <td>{seg.definition?.type === 1 ? 'Static' : seg.definition?.type === 3 ? 'Composite' : 'Dynamic'}</td>
                      <td>{seg.cachedCount || '-'}</td>
                      <td>
                        <button onClick={() => handleEvaluate(seg.id)}><Play size={14} /></button>
                        <button onClick={() => handleDelete(seg.id)}><Trash2 size={14} /></button>
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
      </div>
    </DndContext>
  );
}

export default App;
