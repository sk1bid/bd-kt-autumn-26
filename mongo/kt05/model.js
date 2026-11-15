// Диагностические осмотры бортовых устройств.
const catalog = db.getSiblingDB("transport_inspections");
const text = {bsonType:"string", minLength:1};
const schema = (required, properties) => ({bsonType:"object", required, properties});
const definitions = {
  procedures: schema(["title","steps"], {title:text, steps:{bsonType:"array",
    minItems:1, maxItems:32, uniqueItems:true, items:text}}),
  symptoms: schema(["label"], {label:text}),
  inspections: schema(["deviceId","procedureId","observedAt","findings"], {
    deviceId:{bsonType:"long",minimum:1}, procedureId:{bsonType:"objectId"},
    observedAt:{bsonType:"date"}, findings:{bsonType:"array",minItems:1,maxItems:32,
      items:schema(["step","outcome","symptomIds"], {step:text,outcome:{enum:["pass","fault"]},
        symptomIds:{bsonType:"array",maxItems:8,uniqueItems:true,items:{bsonType:"objectId"}}})}
  })
};
for (const [name, spec] of Object.entries(definitions)) {
  const options = {validator:{$jsonSchema:spec},validationLevel:"strict",validationAction:"error"};
  const command = catalog.getCollectionInfos({name}).length ? {collMod:name} : {create:name};
  const result = catalog.runCommand({...command,...options});
  if (result.ok !== 1) throw new Error(result.errmsg);
}
