load("/examples/model.js");
const assert = require("node:assert/strict");
const c = db.getSiblingDB("transport_inspections");
const proc = new ObjectId(), symptom = new ObjectId(), inspection = new ObjectId();
try {
  c.procedures.insertOne({_id:proc,title:"Проверка GPS",steps:["Получить координаты"]});
  c.symptoms.insertOne({_id:symptom,label:"Нет сигнала"});
  c.inspections.insertOne({_id:inspection,deviceId:NumberLong("1"),procedureId:proc,observedAt:new Date(),
    findings:[{step:"Получить координаты",outcome:"fault",symptomIds:[symptom]}]});
  assert.equal(c.procedures.findOne({_id:proc}).steps.length,1);
  assert.equal(c.symptoms.findOne({_id:symptom}).label,"Нет сигнала");
  assert.equal(c.inspections.findOne({_id:inspection}).findings[0].symptomIds[0].toHexString(),symptom.toHexString());
  assert.equal(c.procedures.updateOne({_id:proc},{$set:{title:"Осмотр GNSS"}}).modifiedCount,1);
  assert.equal(c.symptoms.updateOne({_id:symptom},{$set:{label:"Сигнал отсутствует"}}).modifiedCount,1);
  assert.equal(c.inspections.updateOne({_id:inspection},{$set:{note:"Проверена антенна"}}).modifiedCount,1);
  for (const patch of [{deviceId:"1"},{deviceId:NumberLong("0")},{findings:[]}]) {
    let code;
    try { c.inspections.updateOne({_id:inspection},{$set:patch}); } catch(e) { code=e.code; }
    assert.equal(code,121);
  }
  let missingFieldCode;
  try { c.inspections.updateOne({_id:inspection},{$unset:{procedureId:1}}); }
  catch (e) { missingFieldCode=e.code; }
  assert.equal(missingFieldCode,121);
  assert.equal(c.inspections.deleteOne({_id:inspection}).deletedCount,1);
  assert.equal(c.symptoms.deleteOne({_id:symptom}).deletedCount,1);
  assert.equal(c.procedures.deleteOne({_id:proc}).deletedCount,1);
  print("PASS: CRUD across three collections and four rejected invalid updates");
} finally {
  c.inspections.deleteOne({_id:inspection}); c.symptoms.deleteOne({_id:symptom}); c.procedures.deleteOne({_id:proc});
}
