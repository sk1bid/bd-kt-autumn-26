SELECT 'CREATE DATABASE transport_kt03 TEMPLATE template0'
WHERE NOT EXISTS (SELECT 1 FROM pg_database WHERE datname = 'transport_kt03')
\gexec
