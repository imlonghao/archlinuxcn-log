#!/bin/bash

for package in /home/imlonghao/public_html/log/*
do
  cd $package
  ls -t | tail -n +16 | ifne xargs rm
done
